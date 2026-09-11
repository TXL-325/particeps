package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"particeps/internal/auth"
	"particeps/internal/cgroupcap"
	"particeps/internal/cpu"
	"particeps/internal/incusx"
	"particeps/internal/store"
)

type Instance struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	IncusName      string   `json:"incusName"`
	Image          string   `json:"image"`
	CPUCores       float64  `json:"cpuCores"`
	CPUPin         string   `json:"cpuPin"`
	MemoryMiB      int      `json:"memoryMib"`
	DiskGiB        int      `json:"diskGib"`
	BandwidthMbps  int      `json:"bandwidthMbps"`
	StackMode      string   `json:"stackMode"`
	DesiredPower   string   `json:"desiredPower"`
	NATIPv4        string   `json:"natIpv4"`
	DedicatedIPv4  string   `json:"dedicatedIpv4"`
	IPv6           string   `json:"ipv6"`
	IPv6Mode       string   `json:"ipv6Mode"`
	Status         string   `json:"status"`
	ResourceStatus string   `json:"resourceStatus"`
	CPUUsed        float64  `json:"cpuUsed"`
	MemUsed        int64    `json:"memUsed"`
	RxBps          *float64 `json:"rxBps"`
	TxBps          *float64 `json:"txBps"`
	CPUQuality     string   `json:"cpuQuality"`
	NetworkQuality string   `json:"networkQuality"`
	Ports          []Port   `json:"ports,omitempty"`
}

type Port struct {
	Number   int    `json:"number"`
	Proto    string `json:"proto"`
	ListenIP string `json:"listenIp"`
	Target   int    `json:"target"`
}

type Task struct {
	ID     string     `json:"id"`
	Kind   string     `json:"kind"`
	Status string     `json:"status"`
	Items  []TaskItem `json:"items"`
}

type TaskItem struct {
	Name                string `json:"name"`
	InstanceID          string `json:"instanceId,omitempty"`
	Status              string `json:"status"`
	Step                string `json:"step"`
	Error               string `json:"error,omitempty"`
	CredentialAvailable bool   `json:"credentialAvailable,omitempty"`
}

func (a *App) ListInstances() ([]Instance, error) {
	rows, err := a.Store.DB.Query(`SELECT id,name,incus_name,image,cpu_cores,cpu_pin,memory_mib,disk_gib,bandwidth_mbps,stack_mode,desired_power,nat_ipv4,dedicated_ipv4,ipv6,ipv6_mode,
		EXISTS(SELECT 1 FROM resource_updates WHERE instance_id=instances.id) FROM instances ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Instance
	for rows.Next() {
		var in Instance
		var pending bool
		if err := rows.Scan(&in.ID, &in.Name, &in.IncusName, &in.Image, &in.CPUCores, &in.CPUPin, &in.MemoryMiB, &in.DiskGiB, &in.BandwidthMbps, &in.StackMode, &in.DesiredPower, &in.NATIPv4, &in.DedicatedIPv4, &in.IPv6, &in.IPv6Mode, &pending); err != nil {
			return nil, err
		}
		in.ResourceStatus = "ready"
		if pending {
			in.ResourceStatus = "needs-reconciliation"
		}
		if st, err := a.Incus.GetState(in.IncusName); err == nil {
			in.Status = strings.ToLower(st.Status)
			in.MemUsed = st.Memory.Usage
		} else {
			in.Status = "unknown"
		}
		var rx, tx sql.NullFloat64
		in.CPUQuality, in.NetworkQuality = "missing", "missing"
		_ = a.Metrics.DB.QueryRow(`SELECT cpu_cores,rx_bps,tx_bps,quality,COALESCE(network_quality,quality) FROM samples WHERE object=? ORDER BY ts DESC LIMIT 1`, in.ID).
			Scan(&in.CPUUsed, &rx, &tx, &in.CPUQuality, &in.NetworkQuality)
		if rx.Valid && in.NetworkQuality == "ok" {
			in.RxBps = &rx.Float64
		}
		if tx.Valid && in.NetworkQuality == "ok" {
			in.TxBps = &tx.Float64
		}
		out = append(out, in)
	}
	if out == nil {
		out = []Instance{}
	}
	return out, nil
}

func (a *App) GetInstance(id string) (Instance, []Port, error) {
	list, err := a.ListInstances()
	if err != nil {
		return Instance{}, nil, err
	}
	for _, in := range list {
		if in.ID == id || in.Name == id {
			ports, _ := a.instancePorts(in.ID)
			return in, ports, nil
		}
	}
	return Instance{}, nil, fmt.Errorf("instance not found")
}

func (a *App) instancePorts(id string) ([]Port, error) {
	rows, err := a.Store.DB.Query(`SELECT number, proto, listen_ip, target FROM ports WHERE instance_id=? ORDER BY number, proto`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Port
	for rows.Next() {
		var p Port
		if err := rows.Scan(&p.Number, &p.Proto, &p.ListenIP, &p.Target); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (a *App) SubmitCreate(req CreateReq, idem string) (Task, error) {
	if req.Count < 1 {
		if req.Count < 0 {
			return Task{}, fmt.Errorf("count cannot be negative")
		}
		req.Count = 1
	}
	if req.Count > 256 {
		return Task{}, fmt.Errorf("a batch may contain at most 256 instances")
	}
	if strings.TrimSpace(req.Name) == "" {
		return Task{}, fmt.Errorf("instance name is required")
	}
	if req.CPUCores == 0 {
		req.CPUCores = 0.5
	}
	if err := cpu.ValidateQuota(req.CPUCores); err != nil {
		return Task{}, err
	}
	capCores, capStatus := a.CPUCapStatus()
	if req.CPUCores > capCores {
		return Task{}, fmt.Errorf("guest quota %.2f exceeds aggregate cap %.2f", req.CPUCores, capCores)
	}
	if !capStatus.Applied {
		return Task{}, fmt.Errorf("aggregate CPU cap is not available: %s", capStatus.Note)
	}
	canonicalPin, err := cpu.CanonicalPin(req.CPUPin, cpu.AvailableCPUs(), req.CPUCores)
	if err != nil {
		return Task{}, err
	}
	req.CPUPin = canonicalPin
	if req.Image == "" {
		req.Image = "alpine/3.21/cloud"
	}
	defaultMemory, defaultDisk := 128, 1
	if req.Image == "debian/13/cloud" {
		defaultMemory, defaultDisk = 256, 4
	}
	if req.MemoryMiB == 0 {
		req.MemoryMiB = defaultMemory
	}
	if req.DiskGiB == 0 {
		req.DiskGiB = defaultDisk
	}
	if req.MemoryMiB < 1 || req.DiskGiB < 1 || req.BandwidthMbps < 0 {
		return Task{}, fmt.Errorf("resource limits are invalid")
	}
	if strings.ContainsAny(req.Password, "\r\n\x00") {
		return Task{}, fmt.Errorf("password must be a single line")
	}
	if req.StackMode == "" {
		req.StackMode = a.defaultStack()
	}
	if err := a.validateStack(req.StackMode); err != nil {
		return Task{}, err
	}
	h := hashReq(req)
	if idem != "" {
		var id, status, rh string
		err := a.Store.DB.QueryRow(`SELECT id, status, request_hash FROM tasks WHERE idempotency_key=?`, idem).Scan(&id, &status, &rh)
		if err == nil {
			if rh != h {
				return Task{}, fmt.Errorf("idempotency key reused with different request")
			}
			return a.GetTask(id)
		}
	}
	tid := store.NewID()
	now := store.Now()
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO tasks(id,kind,idempotency_key,request_hash,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		tid, "create", nullIfEmpty(idem), h, "running", now, now); err != nil {
		_ = tx.Rollback()
		if idem != "" {
			var existingID, existingHash string
			if lookupErr := a.Store.DB.QueryRow(`SELECT id,request_hash FROM tasks WHERE idempotency_key=?`, idem).Scan(&existingID, &existingHash); lookupErr == nil && existingHash == h {
				return a.GetTask(existingID)
			}
		}
		return Task{}, err
	}
	names := make([]string, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		name := req.Name
		if req.Count > 1 {
			name = fmt.Sprintf("%s-%d", req.Name, i+1)
		}
		if _, err := tx.Exec(`INSERT INTO task_items(task_id,name,status,step) VALUES(?,?,?,?)`, tid, name, "pending", "queued"); err != nil {
			return Task{}, err
		}
		names = append(names, name)
	}
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	for _, name := range names {
		go a.runCreateItem(tid, name, req)
	}
	return a.GetTask(tid)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (a *App) defaultStack() string {
	p := a.Pool()
	has4 := len(p.IPv4) > 0 || p.NATIPv4 != ""
	has6 := len(p.IPv6) > 0 || len(p.Prefixes) > 0
	switch {
	case has4 && has6:
		return "dual"
	case has6:
		return "v6"
	default:
		return "v4"
	}
}

func (a *App) validateStack(mode string) error {
	p := a.Pool()
	has4 := p.NATIPv4 != "" || len(p.IPv4) > 0
	has6 := len(p.Prefixes) > 0 || len(p.IPv6) > 0 || p.NAT66 != ""
	switch mode {
	case "v4":
		if !has4 {
			return fmt.Errorf("IPv4-only requires a pooled IPv4")
		}
	case "v6":
		if !has6 {
			return fmt.Errorf("IPv6-only requires pooled IPv6 or prefix")
		}
	case "dual":
		if !has4 || !has6 {
			return fmt.Errorf("dual stack requires both IPv4 and IPv6 resources")
		}
	default:
		return fmt.Errorf("unknown stack mode %s", mode)
	}
	return nil
}

func (a *App) runCreateItem(taskID, name string, req CreateReq) {
	a.sem <- struct{}{}
	defer func() { <-a.sem }()
	set := func(status, step, errS, id, extra string) {
		_, _ = a.Store.DB.Exec(`UPDATE task_items SET status=?, step=?, error=?, instance_id=?, result_json=? WHERE task_id=? AND name=?`,
			status, step, errS, id, extra, taskID, name)
		a.refreshTask(taskID)
	}
	set("running", "prepare", "", "", "")
	id := store.NewID()
	incusName := "p-" + id[:12]
	pwLogin := true
	if req.PasswordLogin != nil {
		pwLogin = *req.PasswordLogin
	}
	if !pwLogin && strings.TrimSpace(req.SSHPubKey) == "" {
		set("failed", "prepare", "pubkey-only requires an SSH public key", "", "")
		return
	}
	plain := req.Password
	if plain == "" && pwLogin {
		plain = auth.NewTokenPlain()[:16]
	}
	pool := a.Pool()
	nat4 := pool.NATIPv4
	if nat4 == "" && len(pool.IPv4) > 0 {
		nat4 = pool.IPv4[0]
	}
	v6mode, v6addr := "", ""
	if req.StackMode != "v4" {
		if len(pool.Prefixes) > 0 {
			v6mode = "dedicated"
			v6addr = assignFromPrefix(pool.Prefixes[0], id)
		} else if pool.NAT66 != "" {
			v6mode = "nat66"
			v6addr = pool.NAT66
		} else if len(pool.IPv6) > 0 {
			v6mode = "nat66"
			v6addr = pool.IPv6[0]
		}
	}
	if err := a.reserveInstance(id, name, incusName, req, pwLogin, nat4, v6addr, v6mode); err != nil {
		set("failed", "reserve", err.Error(), "", "")
		return
	}
	cfg := map[string]string{
		"limits.cpu.allowance": cpu.Allowance(req.CPUCores),
		"limits.memory":        fmt.Sprintf("%dMiB", req.MemoryMiB),
		"boot.autostart":       "true",
		"user.particeps.id":    id,
		"raw.lxc":              cgroupcap.LXCRaw(),
		"security.nesting":     "false",
	}
	if req.CPUPin != "" {
		cfg["limits.cpu"] = req.CPUPin
	}
	devices := map[string]map[string]string{
		"root": {"type": "disk", "pool": a.Cfg.StoragePool, "path": "/", "size": fmt.Sprintf("%dGiB", req.DiskGiB)},
	}
	if req.StackMode != "v6" {
		eth := map[string]string{"type": "nic", "network": a.Cfg.Network, "name": "eth0"}
		if req.BandwidthMbps > 0 {
			lim := fmt.Sprintf("%dmbit", req.BandwidthMbps)
			eth["limits.ingress"] = lim
			eth["limits.egress"] = lim
		}
		devices["eth0"] = eth
	}
	set("running", "create", "", id, "")
	if err := a.Incus.CreateInstance(incusName, req.Image, cfg, devices); err != nil {
		set("failed", "create", err.Error(), id, "")
		return
	}
	set("running", "start", "", id, "")
	if err := a.Incus.SetState(incusName, "start", false); err != nil {
		set("failed", "start", err.Error(), id, "")
		return
	}
	time.Sleep(3 * time.Second)
	if pwLogin && plain != "" {
		set("running", "password", "", id, "")
		if err := a.Incus.SetRootPassword(incusName, plain); err != nil {
			set("failed", "password", err.Error(), id, "")
			return
		}
	}
	if req.SSHPubKey != "" {
		set("running", "ssh", "", id, "")
		if err := a.Incus.InstallRootKey(incusName, req.SSHPubKey); err != nil {
			set("failed", "ssh", err.Error(), id, "")
			return
		}
	}
	if req.StackMode != "v6" && nat4 != "" {
		set("running", "forwards", "", id, "")
		if err := a.applyForwards(id, incusName, nat4); err != nil {
			set("failed", "forwards", err.Error(), id, "")
			return
		}
	}
	extra := ""
	if pwLogin {
		var err error
		extra, err = a.sealCredential(taskID, id, plain)
		if err != nil {
			set("failed", "credential", err.Error(), id, "")
			return
		}
	}
	set("ok", "done", "", id, extra)
}

func (a *App) applyForwards(instanceID, incusName, listen string) error {
	st, err := a.Incus.GetState(incusName)
	if err != nil {
		return err
	}
	guestIP := incusx.GuestIPv4(st)
	if guestIP == "" {
		time.Sleep(2 * time.Second)
		st, err = a.Incus.GetState(incusName)
		if err != nil {
			return err
		}
		guestIP = incusx.GuestIPv4(st)
	}
	if guestIP == "" {
		return fmt.Errorf("no guest ipv4 yet")
	}
	ports, _ := a.instancePorts(instanceID)
	if len(ports) == 0 {
		return nil
	}
	return a.Incus.CreateForward(a.Cfg.Network, toForward(listen, guestIP, ports))
}

func toForward(listen, guest string, ports []Port) interface{} {
	type p struct {
		Protocol      string `json:"protocol"`
		ListenPort    string `json:"listen_port"`
		TargetPort    string `json:"target_port"`
		TargetAddress string `json:"target_address"`
	}
	var ps []p
	for _, x := range ports {
		ps = append(ps, p{x.Proto, fmt.Sprintf("%d", x.Number), fmt.Sprintf("%d", x.Target), guest})
	}
	return map[string]any{"listen_address": listen, "ports": ps}
}

func assignFromPrefix(pfx, id string) string {
	// pfx like 2001:db8::/64 — place a host from id hex in the last 16 bits
	base := strings.Split(pfx, "/")[0]
	base = strings.TrimRight(base, ":")
	if !strings.HasSuffix(base, ":") {
		base += ":"
	}
	h := id[:4]
	return base + h
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (a *App) refreshTask(id string) {
	// Aggregate and update in one statement so a worker cannot publish an old
	// snapshot after another worker has already completed the task.
	_, _ = a.Store.DB.Exec(`UPDATE tasks SET status=(SELECT CASE
		WHEN COUNT(*)=0 OR SUM(status IN ('pending','running'))>0 THEN 'running'
		WHEN SUM(status='failed')=0 THEN 'ok'
		WHEN SUM(status='ok')=0 THEN 'failed'
		ELSE 'partial' END FROM task_items WHERE task_id=tasks.id),
		updated_at=? WHERE id=?`, store.Now(), id)
}

func (a *App) GetTask(id string) (Task, error) {
	t := Task{Items: []TaskItem{}}
	err := a.Store.DB.QueryRow(`SELECT id, kind, status FROM tasks WHERE id=?`, id).Scan(&t.ID, &t.Kind, &t.Status)
	if err != nil {
		return t, err
	}
	rows, err := a.Store.DB.Query(`SELECT name, COALESCE(instance_id,''), status, step, error, result_json FROM task_items WHERE task_id=?`, id)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	for rows.Next() {
		var it TaskItem
		var extra string
		if err := rows.Scan(&it.Name, &it.InstanceID, &it.Status, &it.Step, &it.Error, &extra); err != nil {
			return t, err
		}
		if extra != "" {
			var result taskResult
			if json.Unmarshal([]byte(extra), &result) == nil && result.Credential != nil {
				it.CredentialAvailable = result.Credential.ExpiresAt > time.Now().Unix()
			}
		}
		t.Items = append(t.Items, it)
	}
	return t, nil
}

func (a *App) Power(id, action string, force bool) error {
	in, _, err := a.GetInstance(id)
	if err != nil {
		return err
	}
	act := action
	if action == "stop" && force {
		act = "stop"
	}
	if err := a.Incus.SetState(in.IncusName, act, force); err != nil {
		msg := err.Error()
		if action == "start" && strings.Contains(msg, "already running") {
			return nil
		}
		if action == "stop" && strings.Contains(msg, "already stopped") {
			return nil
		}
		return err
	}
	desired := "running"
	if action == "stop" {
		desired = "stopped"
	}
	_, _ = a.Store.DB.Exec(`UPDATE instances SET desired_power=? WHERE id=?`, desired, in.ID)
	return nil
}

func (a *App) DeleteInstance(id string) error {
	in, _, err := a.GetInstance(id)
	if err != nil {
		return err
	}
	_ = a.Incus.SetState(in.IncusName, "stop", true)
	if in.NATIPv4 != "" {
		var others int
		_ = a.Store.DB.QueryRow(`SELECT COUNT(*) FROM instances WHERE nat_ipv4=? AND id!=?`, in.NATIPv4, in.ID).Scan(&others)
		if others == 0 {
			_ = a.Incus.DeleteForward(a.Cfg.Network, in.NATIPv4)
		}
	}
	if err := a.Incus.DeleteInstance(in.IncusName); err != nil && !strings.Contains(err.Error(), "not found") {
		return err
	}
	_, _ = a.Store.DB.Exec(`DELETE FROM ports WHERE instance_id=?`, in.ID)
	_, _ = a.Store.DB.Exec(`DELETE FROM instances WHERE id=?`, in.ID)
	return nil
}

func (a *App) Rebuild(id, image string) error {
	in, _, err := a.GetInstance(id)
	if err != nil {
		return err
	}
	if image == "" {
		image = in.Image
	}
	_ = a.Incus.SetState(in.IncusName, "stop", true)
	if err := a.Incus.Rebuild(in.IncusName, image); err != nil {
		_, _ = a.Store.DB.Exec(`INSERT INTO tasks(id,kind,request_hash,status,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			store.NewID(), "rebuild", in.ID, "needs-review", store.Now(), store.Now())
		return err
	}
	_ = a.Incus.SetState(in.IncusName, "start", false)
	_, _ = a.Store.DB.Exec(`UPDATE instances SET image=? WHERE id=?`, image, in.ID)
	return nil
}

func (a *App) PatchResources(id string, cpuCores *float64, pin *string, mem *int, disk *int, bw *int) error {
	a.resourceMu.Lock()
	defer a.resourceMu.Unlock()
	if err := a.reconcileResourcesLocked(); err != nil {
		return err
	}
	in, _, err := a.GetInstance(id)
	if err != nil {
		return err
	}
	next := in
	if cpuCores != nil {
		next.CPUCores = *cpuCores
	}
	if pin != nil {
		next.CPUPin = *pin
	}
	if mem != nil {
		next.MemoryMiB = *mem
	}
	if disk != nil {
		next.DiskGiB = *disk
	}
	if bw != nil {
		next.BandwidthMbps = *bw
	}
	if err := cpu.ValidateQuota(next.CPUCores); err != nil {
		return err
	}
	capCores, capStatus := a.CPUCapStatus()
	if !capStatus.Applied {
		return fmt.Errorf("aggregate CPU cap is not available")
	}
	if next.CPUCores > capCores {
		return fmt.Errorf("guest quota exceeds aggregate cap")
	}
	canonicalPin, err := cpu.CanonicalPin(next.CPUPin, cpu.AvailableCPUs(), next.CPUCores)
	if err != nil {
		return err
	}
	if next.MemoryMiB < 1 {
		return fmt.Errorf("memory limit must be positive")
	}
	if next.DiskGiB < 1 || next.DiskGiB < in.DiskGiB {
		return fmt.Errorf("disk may only increase")
	}
	if next.BandwidthMbps < 0 {
		return fmt.Errorf("bandwidth cannot be negative")
	}
	if cpuCores == nil && pin == nil && mem == nil && disk == nil && bw == nil {
		return fmt.Errorf("no resource changes supplied")
	}
	cfg := map[string]string{}
	if cpuCores != nil {
		cfg["limits.cpu.allowance"] = cpu.Allowance(next.CPUCores)
	}
	if pin != nil || cpuCores != nil {
		cfg["limits.cpu"] = canonicalPin
		next.CPUPin = canonicalPin
	}
	if mem != nil {
		cfg["limits.memory"] = fmt.Sprintf("%dMiB", next.MemoryMiB)
	}
	current, err := a.Incus.GetConfig(in.IncusName)
	if err != nil {
		return err
	}
	devices, err := decodeDevices(current["devices"])
	if err != nil {
		return err
	}
	updates := map[string]map[string]string{}
	if disk != nil {
		root := devices["root"]
		if root == nil || root["type"] != "disk" || root["path"] != "/" {
			return fmt.Errorf("root disk device is missing or invalid")
		}
		root["size"] = fmt.Sprintf("%dGiB", next.DiskGiB)
		updates["root"] = root
	}
	if bw != nil {
		eth := devices["eth0"]
		if eth == nil || eth["type"] != "nic" {
			return fmt.Errorf("instance network device is missing or invalid")
		}
		limit := ""
		if next.BandwidthMbps > 0 {
			limit = fmt.Sprintf("%dmbit", next.BandwidthMbps)
		}
		eth["limits.ingress"], eth["limits.egress"] = limit, limit
		updates["eth0"] = eth
	}
	if err := a.recordResourceUpdate(in, next, current, cfg, updates); err != nil {
		return err
	}
	operation, updateErr := a.Incus.BeginConfigUpdate(in.IncusName, cfg, updates)
	if err := a.recordResourceOperation(in.ID, operation); err != nil {
		return err
	}
	if updateErr != nil {
		return updateErr
	}
	if !operation.Terminal {
		terminal, waitErr := a.Incus.WaitConfigOperation(operation.ID)
		if terminal {
			operation.Terminal = true
			if err := a.recordResourceOperation(in.ID, operation); err != nil {
				return err
			}
		}
		if waitErr != nil {
			return waitErr
		}
		if !terminal {
			return fmt.Errorf("resource operation has not finished; reconciliation required")
		}
	}
	actual, err := a.Incus.GetConfig(in.IncusName)
	if err != nil {
		return fmt.Errorf("resource readback failed: %w", err)
	}
	matches, err := resourceConfigMatches(actual, cfg, updates)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("resource readback mismatch; reconciliation required")
	}
	return a.finishResourceUpdate(in.ID, resourceValuesOf(next))
}

func decodeDevices(value any) (map[string]map[string]string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var devices map[string]map[string]string
	if err := json.Unmarshal(data, &devices); err != nil {
		return nil, fmt.Errorf("invalid Incus devices: %w", err)
	}
	return devices, nil
}

func (a *App) ResetPassword(id, password string) (string, error) {
	in, _, err := a.GetInstance(id)
	if err != nil {
		return "", err
	}
	if password == "" {
		password = auth.NewTokenPlain()[:16]
	}
	if err := a.Incus.SetRootPassword(in.IncusName, password); err != nil {
		return "", err
	}
	return password, nil
}

func (a *App) RestoreDesiredPower() {
	rows, err := a.Store.DB.Query(`SELECT incus_name, desired_power FROM instances`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name, want string
		if err := rows.Scan(&name, &want); err != nil {
			continue
		}
		st, err := a.Incus.GetState(name)
		if err != nil {
			continue
		}
		running := strings.EqualFold(st.Status, "Running")
		if want == "running" && !running {
			_ = a.Incus.SetState(name, "start", false)
		}
		if want == "stopped" && running {
			_ = a.Incus.SetState(name, "stop", false)
		}
	}
}

func (a *App) Images() []map[string]string {
	rows, err := a.Store.DB.Query(`SELECT alias, source, fingerprint FROM images`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var al, src, fp string
		_ = rows.Scan(&al, &src, &fp)
		out = append(out, map[string]string{"alias": al, "source": src, "fingerprint": fp})
	}
	return out
}

func (a *App) RegisterImage(alias string) error {
	_, err := a.Store.DB.Exec(`INSERT INTO images(alias,source,registered) VALUES(?,?,1) ON CONFLICT(alias) DO UPDATE SET registered=1`, alias, "local")
	return err
}

func (a *App) Series(object, from, to string) ([]map[string]any, error) {
	q := `SELECT ts,cpu_cores,rx_bps,tx_bps,quality,COALESCE(network_quality,quality),quota FROM samples WHERE object=?`
	args := []any{object}
	if from != "" {
		q += ` AND ts>=?`
		args = append(args, from)
	}
	if to != "" {
		q += ` AND ts<=?`
		args = append(args, to)
	}
	q += ` ORDER BY ts`
	rows, err := a.Metrics.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ts int64
		var cpuU, quota float64
		var rx, tx sql.NullFloat64
		var qlt, networkQuality string
		if err := rows.Scan(&ts, &cpuU, &rx, &tx, &qlt, &networkQuality, &quota); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"ts": ts, "cpuCores": cpuU, "rxBps": nullableRate(rx), "txBps": nullableRate(tx), "quality": qlt, "networkQuality": networkQuality, "quota": quota, "quotaPercent": 0})
		if quota > 0 {
			out[len(out)-1]["quotaPercent"] = cpuU / quota * 100
		}
	}
	return out, nil
}

func (a *App) TokenCreate(name, role string) (id, plain string, err error) {
	if role != "read" && role != "manage" {
		return "", "", fmt.Errorf("token role must be read or manage")
	}
	plain = auth.NewTokenPlain()
	id = store.NewID()
	_, err = a.Store.DB.Exec(`INSERT INTO tokens(id,name,hash,role,created_at) VALUES(?,?,?,?,?)`,
		id, name, auth.HashToken(plain), role, store.Now())
	return id, plain, err
}

func (a *App) TokenRevoke(id string) error {
	_, err := a.Store.DB.Exec(`UPDATE tokens SET revoked=1 WHERE id=?`, id)
	return err
}

func (a *App) Tokens() []map[string]any {
	rows, err := a.Store.DB.Query(`SELECT id,name,role,created_at,revoked FROM tokens`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, role string
		var created int64
		var revoked int
		_ = rows.Scan(&id, &name, &role, &created, &revoked)
		out = append(out, map[string]any{"id": id, "name": name, "role": role, "createdAt": created, "revoked": revoked == 1})
	}
	return out
}

var _ = sql.ErrNoRows
