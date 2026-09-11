package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"particeps/internal/auth"
	"particeps/internal/cgroupcap"
	"particeps/internal/config"
	"particeps/internal/incusx"
	"particeps/internal/store"
)

type resourceBackend struct {
	*incusx.Client
	config        map[string]string
	devices       map[string]map[string]string
	patchError    error
	patches       int
	ignorePatch   bool
	startError    error
	starts        int
	clearedCgroup bool
	readbackError error
	deferPatch    bool
	operationDone bool
	unknownPatch  bool
}

func (b *resourceBackend) GetState(string) (*incusx.InstanceState, error) {
	return &incusx.InstanceState{Status: "Running", StatusCode: 103}, nil
}
func (b *resourceBackend) GetConfig(string) (map[string]any, error) {
	if b.patches > 0 && b.readbackError != nil {
		return nil, b.readbackError
	}
	data, _ := json.Marshal(map[string]any{"config": b.config, "devices": b.devices})
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	return result, nil
}
func (b *resourceBackend) BeginConfigUpdate(name string, config map[string]string, devices map[string]map[string]string) (incusx.ConfigOperation, error) {
	if b.unknownPatch {
		b.patches++
		return incusx.ConfigOperation{}, errors.New("connection lost before receiving operation reference")
	}
	if b.deferPatch {
		b.patches++
		return incusx.ConfigOperation{ID: "/1.0/operations/resource-test"}, nil
	}
	err := b.patchConfig(name, config, devices)
	return incusx.ConfigOperation{Terminal: true}, err
}
func (b *resourceBackend) WaitConfigOperation(string) (bool, error) {
	if b.operationDone {
		return true, nil
	}
	return false, errors.New("operation wait timeout")
}
func (b *resourceBackend) ConfigOperationFinished(string) (bool, error) { return b.operationDone, nil }

func (b *resourceBackend) patchConfig(_ string, config map[string]string, devices map[string]map[string]string) error {
	b.patches++
	if raw, exists := config["raw.lxc"]; exists && raw == "" {
		b.clearedCgroup = true
	}
	if b.patchError != nil {
		return b.patchError
	}
	if b.ignorePatch {
		return nil
	}
	for name, device := range devices {
		if device["type"] == "" {
			return fmt.Errorf("device %s has no type", name)
		}
		// Incus replaces the complete value of a supplied device name.
		b.devices[name] = device
	}
	for key, value := range config {
		b.config[key] = value
	}
	return nil
}
func (b *resourceBackend) CreateInstance(_ string, _ string, config map[string]string, devices map[string]map[string]string) error {
	b.config, b.devices = config, devices
	return nil
}
func (b *resourceBackend) SetState(_, action string, _ bool) error {
	if action == "start" {
		b.starts++
		if b.starts == 1 {
			return b.startError
		}
	}
	return nil
}

func testApp(t *testing.T) (*App, *resourceBackend) {
	t.Helper()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.CPUCapCores = 2
	s, err := store.Open(filepath.Join(cfg.DataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Open(filepath.Join(cfg.DataDir, "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(); _ = m.Close() })
	err = initMetrics(m.DB)
	if err != nil {
		t.Fatal(err)
	}
	b := &resourceBackend{
		config: map[string]string{"limits.cpu.allowance": "50ms/100ms", "limits.memory": "128MiB", "boot.autostart": "true"},
		devices: map[string]map[string]string{
			"root": {"type": "disk", "path": "/", "pool": "particeps-pool", "size": "4GiB"},
			"eth0": {"type": "nic", "network": "particepsbr0", "name": "eth0"},
		},
	}
	a := &App{Cfg: cfg, Store: s, Metrics: m, Auth: &auth.Auth{S: s}, Incus: b, Cap: cgroupcap.Status{Applied: true, Cores: 2}, sem: make(chan struct{}, 2)}
	_, err = s.DB.Exec(`INSERT INTO instances(id,name,incus_name,display_name,image,cpu_cores,memory_mib,disk_gib,stack_mode,desired_power,created_at)
		VALUES('guest','guest','p-guest','guest','alpine/3.21/cloud',0.5,128,4,'v4','running',0)`)
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

func quotaInDB(t *testing.T, a *App) float64 {
	t.Helper()
	var quota float64
	if err := a.Store.DB.QueryRow(`SELECT cpu_cores FROM instances WHERE id='guest'`).Scan(&quota); err != nil {
		t.Fatal(err)
	}
	return quota
}

func TestResourceValidationHasNoSideEffects(t *testing.T) {
	a, b := testApp(t)
	quota, disk := 1.0, 2
	if err := a.PatchResources("guest", &quota, nil, nil, &disk, nil); err == nil {
		t.Fatal("accepted disk shrink")
	}
	if got := quotaInDB(t, a); got != 0.5 {
		t.Fatalf("rejected request changed CPU record to %v", got)
	}
	if b.patches != 0 {
		t.Fatal("rejected request changed backend")
	}
}

func TestFailedResourceChangePreservesStoredValues(t *testing.T) {
	for _, field := range []string{"cpu", "memory", "disk", "bandwidth"} {
		t.Run(field, func(t *testing.T) {
			a, b := testApp(t)
			b.patchError = errors.New("backend rejected update")
			quota, memory, disk, bandwidth := 1.0, 256, 8, 20
			var err error
			switch field {
			case "cpu":
				err = a.PatchResources("guest", &quota, nil, nil, nil, nil)
			case "memory":
				err = a.PatchResources("guest", nil, nil, &memory, nil, nil)
			case "disk":
				err = a.PatchResources("guest", nil, nil, nil, &disk, nil)
			case "bandwidth":
				err = a.PatchResources("guest", nil, nil, nil, nil, &bandwidth)
			}
			if err == nil {
				t.Fatal("backend failure reported as success")
			}
			var gotCPU float64
			var gotMem, gotDisk, gotBW int
			err = a.Store.DB.QueryRow(`SELECT cpu_cores,memory_mib,disk_gib,bandwidth_mbps FROM instances WHERE id='guest'`).Scan(&gotCPU, &gotMem, &gotDisk, &gotBW)
			if err != nil {
				t.Fatal(err)
			}
			if gotCPU != 0.5 || gotMem != 128 || gotDisk != 4 || gotBW != 0 {
				t.Fatalf("failed update persisted: %v %d %d %d", gotCPU, gotMem, gotDisk, gotBW)
			}
		})
	}
}

func TestResourceChangePreservesCompleteDevices(t *testing.T) {
	a, b := testApp(t)
	disk, bw := 8, 20
	if err := a.PatchResources("guest", nil, nil, nil, &disk, &bw); err != nil {
		t.Fatal(err)
	}
	if b.patches != 1 {
		t.Fatalf("expected one atomic backend update, got %d", b.patches)
	}
	if b.devices["root"]["path"] != "/" || b.devices["root"]["pool"] != "particeps-pool" || b.devices["eth0"]["network"] != "particepsbr0" {
		t.Fatal("lost existing device properties")
	}
}

func TestResourceChangeRequiresReadback(t *testing.T) {
	a, b := testApp(t)
	b.ignorePatch = true
	quota := 1.0
	if err := a.PatchResources("guest", &quota, nil, nil, nil, nil); err == nil {
		t.Fatal("backend did not apply quota but update succeeded")
	}
	if quotaInDB(t, a) != 0.5 {
		t.Fatal("unverified quota persisted")
	}
}

func TestCreateNeverDropsCgroupAfterStartFailure(t *testing.T) {
	a, b := testApp(t)
	b.startError = errors.New("cannot start with configured cgroup")
	if err := a.SetPool(PoolSettings{IPv4: []string{"192.0.2.1"}, NATIPv4: "192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	_, err := a.Store.DB.Exec(`INSERT INTO tasks(id,kind,request_hash,status,created_at,updated_at) VALUES('batch','create','h','running',0,0);
		INSERT INTO task_items(task_id,name,status,step) VALUES('batch','new-guest','pending','queued')`)
	if err != nil {
		t.Fatal(err)
	}
	a.runCreateItem("batch", "new-guest", CreateReq{Image: "alpine/3.21/cloud", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4"})
	if b.clearedCgroup || b.starts != 1 {
		t.Fatalf("unsafe fallback: cleared=%v, starts=%d", b.clearedCgroup, b.starts)
	}
	task, err := a.GetTask("batch")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "failed" {
		t.Fatalf("start failure task status: %s", task.Status)
	}
}

func TestUncertainResourcesSurviveRestartAndBlockStaleCap(t *testing.T) {
	for _, failure := range []string{"readback", "database"} {
		t.Run(failure, func(t *testing.T) {
			a, b := testApp(t)
			if failure == "readback" {
				b.readbackError = errors.New("readback unavailable")
			} else if _, err := a.Store.DB.Exec(`CREATE TRIGGER reject_resources BEFORE UPDATE ON instances BEGIN SELECT RAISE(ABORT,'database unavailable'); END`); err != nil {
				t.Fatal(err)
			}
			quota := 1.5
			if err := a.PatchResources("guest", &quota, nil, nil, nil, nil); err == nil {
				t.Fatal("unconfirmed update reported success")
			}
			in, _, err := a.GetInstance("guest")
			if err != nil || in.ResourceStatus != "needs-reconciliation" || in.CPUCores != 0.5 {
				t.Fatalf("uncertainty was not exposed: %+v %v", in, err)
			}
			// A fresh App has no in-memory knowledge of the pending change.
			restarted := &App{Cfg: a.Cfg, Store: a.Store, Metrics: a.Metrics, Incus: b, Cap: a.Cap}
			applied := false
			restarted.applyCap = func(cores float64) cgroupcap.Status {
				applied = true
				return cgroupcap.Status{Cores: cores, Applied: true}
			}
			if err := restarted.SetCPUCap(1); err == nil || applied {
				t.Fatal("stale database quota allowed a lower cap")
			}
			b.readbackError = nil
			if _, err := a.Store.DB.Exec(`DROP TRIGGER IF EXISTS reject_resources`); err != nil {
				t.Fatal(err)
			}
			if err := restarted.SetCPUCap(1); err == nil || applied {
				t.Fatal("reconciled quota allowed a lower cap")
			}
			if got := quotaInDB(t, a); got != quota {
				t.Fatalf("actual quota was not reconciled: %v", got)
			}
			in, _, err = a.GetInstance("guest")
			if err != nil || in.ResourceStatus != "ready" {
				t.Fatalf("verified resource update was not finalized: %+v %v", in, err)
			}
		})
	}
}

func TestPartialResourceApplicationRemainsBlocked(t *testing.T) {
	a, b := testApp(t)
	b.ignorePatch = true
	quota, memory := 1.5, 256
	if err := a.PatchResources("guest", &quota, nil, &memory, nil, nil); err == nil {
		t.Fatal("ignored update reported success")
	}
	b.config["limits.cpu.allowance"] = "150ms/100ms"
	a.applyCap = func(float64) cgroupcap.Status {
		t.Fatal("cap changed with partially applied resources")
		return cgroupcap.Status{}
	}
	if err := a.SetCPUCap(2); err == nil {
		t.Fatal("partial resource update was silently discarded")
	}
	if got := quotaInDB(t, a); got != 0.5 {
		t.Fatalf("unverified values saved: %v", got)
	}
}

func TestRejectedResourceUpdateCanReconcilePreviousState(t *testing.T) {
	a, b := testApp(t)
	b.patchError = errors.New("rejected")
	quota := 1.5
	if err := a.PatchResources("guest", &quota, nil, nil, nil, nil); err == nil {
		t.Fatal("rejected update reported success")
	}
	a.applyCap = func(cores float64) cgroupcap.Status { return cgroupcap.Status{Cores: cores, Applied: true} }
	if err := a.SetCPUCap(1); err != nil {
		t.Fatal(err)
	}
	if got := quotaInDB(t, a); got != 0.5 {
		t.Fatalf("rejected quota persisted: %v", got)
	}
}

func TestTimedOutResourceOperationCannotReconcileBeforeItFinishes(t *testing.T) {
	a, b := testApp(t)
	b.deferPatch = true
	quota := 1.5
	if err := a.PatchResources("guest", &quota, nil, nil, nil, nil); err == nil {
		t.Fatal("operation wait timeout reported success")
	}
	// Restart with only durable state. The currently unchanged backend does
	// not prove the previous request cannot still take effect.
	a = &App{Cfg: a.Cfg, Store: a.Store, Metrics: a.Metrics, Incus: b, Cap: a.Cap}
	a.applyCap = func(float64) cgroupcap.Status {
		t.Fatal("cap changed while the resource operation was unresolved")
		return cgroupcap.Status{}
	}
	if err := a.SetCPUCap(1); err == nil {
		t.Fatal("running operation was discarded based on old config")
	}
	b.config["limits.cpu.allowance"] = "150ms/100ms"
	if err := a.SetCPUCap(1); err == nil {
		t.Fatal("running operation was discarded based on new config")
	}
	b.operationDone = true
	if err := a.SetCPUCap(1); err == nil {
		t.Fatal("cap below the completed guest quota was accepted")
	}
	if got := quotaInDB(t, a); got != quota {
		t.Fatalf("completed operation was not reconciled: %v", got)
	}
}

func TestUnknownResourceOperationRemainsBlocked(t *testing.T) {
	a, b := testApp(t)
	b.unknownPatch = true
	quota := 1.5
	if err := a.PatchResources("guest", &quota, nil, nil, nil, nil); err == nil {
		t.Fatal("lost response reported success")
	}
	a.applyCap = func(float64) cgroupcap.Status {
		t.Fatal("cap changed despite an unknown operation")
		return cgroupcap.Status{}
	}
	if err := a.SetCPUCap(1); err == nil {
		t.Fatal("lost operation reference was ignored")
	}
}
