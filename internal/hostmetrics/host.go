package hostmetrics

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Snapshot struct {
	Hostname       string  `json:"hostname"`
	UptimeSec      float64 `json:"uptimeSec"`
	Load1          float64 `json:"load1"`
	Load5          float64 `json:"load5"`
	Load15         float64 `json:"load15"`
	LogicalCPUs    int     `json:"logicalCpus"`
	CPUUsedCores   float64 `json:"cpuUsedCores"`
	CPUPercent     float64 `json:"cpuPercent"`
	MemTotal       uint64  `json:"memTotal"`
	MemAvail       uint64  `json:"memAvail"`
	DiskTotal      uint64  `json:"diskTotal"`
	DiskAvail      uint64  `json:"diskAvail"`
	UplinkRxBps    float64 `json:"uplinkRxBps"`
	UplinkTxBps    float64 `json:"uplinkTxBps"`
	UplinkName     string  `json:"uplinkName"`
	SampledAt      int64   `json:"sampledAt"`
	Quality        string  `json:"quality"`
	CPUQuality     string  `json:"cpuQuality"`
	NetworkQuality string  `json:"networkQuality"`
}

type Sampler struct {
	mu        sync.RWMutex
	latest    Snapshot
	prevCPUOK bool
	prevNetOK bool
	prevCPU   uint64
	prevIdle  uint64
	prevRx    uint64
	prevTx    uint64
	prevAt    time.Time
	uplink    string
}

func NewSampler(uplink string) *Sampler {
	s := &Sampler{uplink: uplink}
	s.Snapshot()
	return s
}

func (s *Sampler) Latest() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

func (s *Sampler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Snapshot{
		LogicalCPUs: runtime.NumCPU(),
		SampledAt:   time.Now().Unix(),
		Quality:     "ok",
		UplinkName:  s.uplink,
	}
	out.Hostname, _ = os.Hostname()
	if u, err := readUptime(); err == nil {
		out.UptimeSec = u
	}
	out.Load1, out.Load5, out.Load15 = readLoad()
	out.MemTotal, out.MemAvail = readMem()
	out.DiskTotal, out.DiskAvail = readDisk("/")
	cpu, idle, cpuOK := readCPU()
	rx, tx, netOK := readNet(s.uplink)
	now := time.Now()
	s.updateRates(&out, now, cpu, idle, cpuOK, rx, tx, netOK)
	s.latest = out
	return out
}

// CPU and interface counters have independent validity and baselines. A
// missing interface must not suppress otherwise valid CPU measurements.
func (s *Sampler) updateRates(out *Snapshot, now time.Time, cpu, idle uint64, cpuOK bool, rx, tx uint64, netOK bool) {
	out.CPUQuality, out.NetworkQuality = "ok", "ok"
	switch {
	case !cpuOK:
		out.CPUQuality = "missing"
	case !s.prevCPUOK:
		out.CPUQuality = "first"
	case cpu <= s.prevCPU || idle < s.prevIdle || idle-s.prevIdle > cpu-s.prevCPU:
		out.CPUQuality = "reset"
	default:
		busy := 1 - float64(idle-s.prevIdle)/float64(cpu-s.prevCPU)
		out.CPUPercent = busy * 100
		out.CPUUsedCores = busy * float64(out.LogicalCPUs)
	}
	dt := now.Sub(s.prevAt).Seconds()
	switch {
	case !netOK:
		out.NetworkQuality = "missing"
	case !s.prevNetOK:
		out.NetworkQuality = "first"
	case rx < s.prevRx || tx < s.prevTx || dt <= 0:
		out.NetworkQuality = "reset"
	default:
		out.UplinkRxBps = float64(rx-s.prevRx) / dt
		out.UplinkTxBps = float64(tx-s.prevTx) / dt
	}
	out.Quality = out.CPUQuality
	if out.Quality == "ok" {
		out.Quality = out.NetworkQuality
	}
	if cpuOK {
		s.prevCPU, s.prevIdle = cpu, idle
	}
	if netOK {
		s.prevRx, s.prevTx = rx, tx
	}
	s.prevAt = now
	s.prevCPUOK, s.prevNetOK = cpuOK, netOK
}

func readUptime() (float64, error) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) < 1 {
		return 0, os.ErrInvalid
	}
	return strconv.ParseFloat(f[0], 64)
}

func readLoad() (float64, float64, float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return 0, 0, 0
	}
	a, _ := strconv.ParseFloat(f[0], 64)
	b5, _ := strconv.ParseFloat(f[1], 64)
	c, _ := strconv.ParseFloat(f[2], 64)
	return a, b5, c
}

func readMem() (uint64, uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var total, avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseKB(line) * 1024
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = parseKB(line) * 1024
		}
	}
	return total, avail
}

func parseKB(line string) uint64 {
	f := strings.Fields(line)
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.ParseUint(f[1], 10, 64)
	return n
}

func readCPU() (total, idle uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0, false
	}
	return parseCPULine(sc.Text())
}

func parseCPULine(line string) (total, idle uint64, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var sum uint64
	// guest and guest_nice are already included in user and nice by Linux.
	for i := 1; i < len(fields) && i <= 8; i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		sum += n
		if i == 4 || i == 5 {
			idle += n
		}
	}
	return sum, idle, true
}

func readNet(iface string) (rx, tx uint64, ok bool) {
	if iface == "" {
		iface = defaultUplink()
	}
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != iface {
			continue
		}
		f := strings.Fields(parts[1])
		if len(f) < 9 {
			continue
		}
		rx, err = strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		tx, err = strconv.ParseUint(f[8], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		return rx, tx, true
	}
	return 0, 0, false
}

func defaultUplink() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "eth0"
	}
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == "00000000" {
			return f[0]
		}
	}
	return "eth0"
}

func DefaultUplink() string { return defaultUplink() }
