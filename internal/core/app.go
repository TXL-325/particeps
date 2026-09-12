package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"particeps/internal/auth"
	"particeps/internal/cgroupcap"
	"particeps/internal/config"
	"particeps/internal/cpu"
	"particeps/internal/hostmetrics"
	"particeps/internal/incusx"
	"particeps/internal/netdetect"
	"particeps/internal/sample"
	"particeps/internal/store"
)

type App struct {
	Cfg     config.Config
	Store   *store.Store
	Metrics *store.Store
	Auth    *auth.Auth
	Incus   IncusBackend
	Host    *hostmetrics.Sampler
	Cap     cgroupcap.Status

	mu           sync.Mutex
	resourceMu   sync.Mutex
	allocationMu sync.Mutex
	hostPorts    func() (map[int]bool, error)
	clearNAT     func([]Port) error
	dirLock      *os.File
	collectStop  chan struct{}
	collectDone  chan struct{}
	closeOnce    sync.Once
	cpuMu        sync.RWMutex
	applyCap     func(float64) cgroupcap.Status
	credentialMu sync.Mutex
	prevHost     sample.Point
	prevInst     map[string]sample.Point
	prevInstAt   map[string]time.Time
	sem          chan struct{}
}

type PoolSettings struct {
	IPv4        []string `json:"ipv4"`
	IPv6        []string `json:"ipv6"`
	Prefixes    []string `json:"prefixes"`
	NATIPv4     string   `json:"natIPv4"`
	NAT66       string   `json:"nat66"`
	DedicatedV4 []string `json:"dedicatedV4"`
}

type CreateReq struct {
	Name          string  `json:"name"`
	Image         string  `json:"image"`
	CPUCores      float64 `json:"cpuCores"`
	CPUPin        string  `json:"cpuPin"`
	MemoryMiB     int     `json:"memoryMib"`
	DiskGiB       int     `json:"diskGib"`
	BandwidthMbps int     `json:"bandwidthMbps"`
	StackMode     string  `json:"stackMode"` // v4 | v6 | dual
	SSHPubKey     string  `json:"sshPubKey"`
	Password      string  `json:"password"`
	PasswordLogin *bool   `json:"passwordLogin"`
	Count         int     `json:"count"`
}

func Open(cfg config.Config) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, err
	}
	lock, err := lockDataDirectory(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			_ = lock.Close()
		}
	}()
	st, err := store.Open(cfg.StateDB())
	if err != nil {
		return nil, err
	}
	mt, err := store.Open(cfg.MetricsDB())
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	if err := initMetrics(mt.DB); err != nil {
		_ = st.Close()
		_ = mt.Close()
		return nil, err
	}
	a := &App{
		Cfg:         cfg,
		Store:       st,
		Metrics:     mt,
		Auth:        &auth.Auth{S: st},
		Host:        hostmetrics.NewSampler(hostmetrics.DefaultUplink()),
		prevInst:    map[string]sample.Point{},
		prevInstAt:  map[string]time.Time{},
		sem:         make(chan struct{}, cfg.TaskConcurrency),
		dirLock:     lock,
		collectStop: make(chan struct{}),
		collectDone: make(chan struct{}),
	}
	capCores := cfg.CPUCapCores
	if v := st.Setting("cpu_cap_cores", ""); v != "" {
		capCores, err = strconv.ParseFloat(v, 64)
		if err != nil {
			_ = st.Close()
			_ = mt.Close()
			return nil, fmt.Errorf("saved CPU cap is invalid")
		}
	}
	if capCores <= 0 {
		capCores = cpu.DefaultAggregateCap(runtime.NumCPU())
		_ = st.SetSetting("cpu_cap_cores", fmt.Sprintf("%g", capCores))
	}
	a.Cfg.CPUCapCores = capCores
	a.Cap = cgroupcap.Apply(capCores)
	a.Incus = incusx.Connect(cfg.IncusSocket, cfg.IncusProject)
	a.seedImages()
	opened = true
	go a.collectLoop()
	return a, nil
}

func (a *App) seedImages() {
	_, _ = a.Store.DB.Exec(`INSERT OR IGNORE INTO images(alias, source, registered) VALUES
		('alpine/3.21/cloud', 'images:', 1),
		('debian/13/cloud', 'images:', 1)`)
}

func (a *App) Close() {
	a.closeOnce.Do(func() {
		if a.collectStop != nil {
			close(a.collectStop)
			<-a.collectDone
		}
		_ = a.Store.Close()
		_ = a.Metrics.Close()
		if a.dirLock != nil {
			_ = a.dirLock.Close()
		}
	})
}

func (a *App) BootstrapAdmin() (string, error) {
	if a.Auth.HasAdmin() {
		return "", nil
	}
	plain := auth.NewTokenPlain()[:20]
	if err := a.Auth.SetAdminPassword(plain); err != nil {
		return "", err
	}
	if err := os.WriteFile(a.Cfg.Bootstrap(), []byte(plain+"\n"), 0600); err != nil {
		return "", err
	}
	log.Printf("admin password written to %s", a.Cfg.Bootstrap())
	return plain, nil
}

func (a *App) HostSnapshot() map[string]any {
	h := a.Host.Latest()
	sumQuota := a.sumQuota()
	capCores, capStatus := a.CPUCapStatus()
	return map[string]any{
		"host":            h,
		"cpuCapCores":     capCores,
		"configuredCores": sumQuota,
		"overcommit":      cpu.OvercommitRatio(sumQuota, float64(h.LogicalCPUs)),
		"guestCap":        capStatus,
		"incus":           a.incusStatus(),
	}
}

func (a *App) incusStatus() map[string]any {
	err := a.Incus.Ready()
	m := map[string]any{"ok": err == nil}
	if err != nil {
		m["error"] = err.Error()
	}
	return m
}

func (a *App) sumQuota() float64 {
	var s float64
	_ = a.Store.DB.QueryRow(`SELECT COALESCE(SUM(cpu_cores),0) FROM instances`).Scan(&s)
	return s
}

func (a *App) DetectedNetwork() []netdetect.Address { return netdetect.Scan() }

func (a *App) Pool() PoolSettings {
	var p PoolSettings
	_ = a.Store.JSONSetting("network_pool", &p)
	return p
}

func (a *App) SetPool(p PoolSettings) error { return a.Store.SetJSONSetting("network_pool", p) }

func (a *App) SetCPUCap(cores float64) error {
	a.resourceMu.Lock()
	defer a.resourceMu.Unlock()
	if err := a.reconcileResourcesLocked(); err != nil {
		return err
	}
	a.cpuMu.Lock()
	defer a.cpuMu.Unlock()
	if err := cpu.ValidateQuota(cores); err != nil {
		return err
	}
	if cores < 0.25 {
		return fmt.Errorf("cpu cap too small")
	}
	var maxGuest float64
	if err := a.Store.DB.QueryRow(`SELECT COALESCE(MAX(cpu_cores),0) FROM instances`).Scan(&maxGuest); err != nil {
		return err
	}
	if maxGuest > cores {
		return fmt.Errorf("cpu cap %.2f is below largest guest quota %.2f", cores, maxGuest)
	}
	apply := a.applyCap
	if apply == nil {
		apply = cgroupcap.Apply
	}
	next := apply(cores)
	if !next.Applied {
		// A write may have succeeded before readback failed. Never keep the old
		// Applied flag unless restoring that value is also verified.
		restored := apply(a.Cfg.CPUCapCores)
		a.Cap = restored
		return fmt.Errorf("CPU cap was not confirmed: %s (previous cap restored: %t)", next.Note, restored.Applied)
	}
	if err := a.Store.SetSetting("cpu_cap_cores", fmt.Sprintf("%g", cores)); err != nil {
		restored := apply(a.Cfg.CPUCapCores)
		a.Cap = restored
		return fmt.Errorf("CPU cap could not be persisted (previous cap restored: %t): %w", restored.Applied, err)
	}
	a.Cfg.CPUCapCores = cores
	a.Cap = next
	return nil
}

func (a *App) CPUCapStatus() (float64, cgroupcap.Status) {
	a.cpuMu.RLock()
	defer a.cpuMu.RUnlock()
	return a.Cfg.CPUCapCores, a.Cap
}

func hashReq(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func (a *App) collectLoop() {
	defer close(a.collectDone)
	t := time.NewTicker(time.Duration(a.Cfg.SampleSeconds) * time.Second)
	defer t.Stop()
	for {
		select {
		case <-a.collectStop:
			return
		case <-t.C:
			a.sampleOnce()
		}
	}
}

func (a *App) sampleOnce() {
	h := a.Host.Snapshot()
	_, _ = a.Metrics.DB.Exec(`INSERT INTO samples(ts,object,cpu_cores,rx_bps,tx_bps,quality,network_quality,quota) VALUES(?,?,?,?,?,?,?,?)`,
		h.SampledAt, "host", h.CPUUsedCores, validRate(h.UplinkRxBps, h.NetworkQuality), validRate(h.UplinkTxBps, h.NetworkQuality), h.CPUQuality, h.NetworkQuality, float64(h.LogicalCPUs))
	rows, err := a.Store.DB.Query(`SELECT id, cpu_cores, incus_name FROM instances`)
	if err != nil {
		return
	}
	defer rows.Close()
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	_, _ = a.Metrics.DB.Exec(`DELETE FROM samples WHERE ts < ?`, cutoff)
	for rows.Next() {
		var id, incusName string
		var quota float64
		if err := rows.Scan(&id, &quota, &incusName); err != nil {
			continue
		}
		st, err := a.Incus.GetState(incusName)
		observedAt := time.Now()
		pt := sample.Point{OK: err == nil}
		if err == nil {
			pt.CPUNs = uint64(st.CPU.Usage)
			pt.Rx, pt.Tx = incusx.GuestNetTotals(st)
			a.refreshForwardAddress(id, st)
		}
		a.mu.Lock()
		prev := a.prevInst[id]
		elapsed := observedAt.Sub(a.prevInstAt[id]).Seconds()
		rate := sample.Delta(prev, pt, elapsed, prev.OK)
		a.prevInst[id] = pt
		a.prevInstAt[id] = observedAt
		a.mu.Unlock()
		_, _ = a.Metrics.DB.Exec(`INSERT INTO samples(ts,object,cpu_cores,rx_bps,tx_bps,quality,network_quality,quota) VALUES(?,?,?,?,?,?,?,?)`,
			time.Now().Unix(), id, rate.Cores, validRate(rate.RxBps, rate.Quality), validRate(rate.TxBps, rate.Quality), rate.Quality, rate.Quality, quota)
	}
}
