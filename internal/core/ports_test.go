package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"particeps/internal/incusx"
	"particeps/internal/store"
)

func reservePortGuest(t *testing.T, a *App, b *forwardBackend, id, address string, udp bool) {
	t.Helper()
	b.addresses["p-"+id] = address
	req := CreateReq{Image: "alpine", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4", AllocateUDP: udp}
	if err := a.reserveInstance(id, id, "p-"+id, req, true, "192.0.2.1", "", ""); err != nil {
		t.Fatal(err)
	}
}

func storedPort(t *testing.T, a *App, id string, number int, proto string) Port {
	t.Helper()
	ports, err := a.instancePorts(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ports {
		if p.Number == number && p.Proto == proto {
			return p
		}
	}
	t.Fatalf("missing reserved port %s %d/%s", id, number, proto)
	return Port{}
}

func TestNewPortReservationsOnlyEnableSSH(t *testing.T) {
	for _, udp := range []bool{false, true} {
		t.Run(fmt.Sprintf("udp-%t", udp), func(t *testing.T) {
			a, b := networkTestApp(t)
			a.Cfg.PortsPerGuest = 20
			reservePortGuest(t, a, b, "a", "10.80.0.10", udp)
			ports, err := a.instancePorts("a")
			wantCount := 20
			if udp {
				wantCount = 40
			}
			if err != nil || len(ports) != wantCount {
				t.Fatalf("reservation rows: got %d want %d, %v", len(ports), wantCount, err)
			}
			for number := 20000; number < 20020; number++ {
				tcp := storedPort(t, a, "a", number, "tcp")
				wantTarget := number
				if number == 20000 {
					wantTarget = 22
				}
				if tcp.Target != wantTarget || tcp.Enabled != (number == 20000) {
					t.Fatalf("wrong default TCP reservation: %+v", tcp)
				}
				if udp {
					p := storedPort(t, a, "a", number, "udp")
					if p.Enabled || p.Target != number {
						t.Fatalf("UDP was automatically bound: %+v", p)
					}
				}
			}
			var cleanup []Port
			a.clearNAT = func(ports []Port) error { cleanup = append(cleanup, ports...); return nil }
			if err := a.SyncPorts("a"); err != nil {
				t.Fatal(err)
			}
			actual := b.forwards["192.0.2.1"].Ports
			if len(actual) != 1 || actual[0].Protocol != "tcp" || actual[0].ListenPort != "20000" || actual[0].TargetPort != "22" || actual[0].TargetAddress != "10.80.0.10" {
				t.Fatalf("default forwarding exposed extra ports: %+v", actual)
			}
			if len(cleanup) != 1 || cleanup[0].Number != 20000 || cleanup[0].Proto != "tcp" || !cleanup[0].directBinding {
				t.Fatalf("cleanup touched an unbound reservation: %+v", cleanup)
			}
			instances, err := a.ListInstances()
			if err != nil || len(instances) != 1 || !reflect.DeepEqual(instances[0].Ports, ports) {
				t.Fatalf("list omitted the reservation ledger: %+v %v", instances, err)
			}
		})
	}
}

func TestReservedNumbersKeepOwnershipWithoutUDPRows(t *testing.T) {
	a, b := networkTestApp(t)
	reservePortGuest(t, a, b, "a", "10.80.0.10", false)
	reservePortGuest(t, a, b, "b", "10.80.0.11", true)
	if got := storedPort(t, a, "b", 20002, "tcp"); got.Number != 20002 {
		t.Fatalf("disabled numbers were reused: %+v", got)
	}
	if _, err := a.Store.DB.Exec(`INSERT INTO ports(instance_id,number,proto,listen_ip,target,enabled) VALUES('b',20001,'udp','192.0.2.1',53,1)`); err == nil {
		t.Fatal("UDP without an allocated row was assigned to a different TCP owner")
	}
	if _, err := a.Store.DB.Exec(`UPDATE ports SET number=20001 WHERE instance_id='b' AND number=20002 AND proto='udp'`); err == nil {
		t.Fatal("updating a UDP reservation bypassed number ownership")
	}
}

func TestPortAppendAndTargetEditRequireExplicitActivation(t *testing.T) {
	for _, udp := range []bool{false, true} {
		t.Run(fmt.Sprintf("udp-%t", udp), func(t *testing.T) {
			a, b := networkTestApp(t)
			reservePortGuest(t, a, b, "a", "10.80.0.10", udp)
			if err := a.SyncPorts("a"); err != nil {
				t.Fatal(err)
			}
			if err := a.AddPort("a", 0); err != nil {
				t.Fatal(err)
			}
			if p := storedPort(t, a, "a", 20002, "tcp"); p.Enabled || p.Target != 20002 {
				t.Fatalf("append activated a TCP mapping: %+v", p)
			}
			wantCount := 3
			if udp {
				wantCount = 6
				if p := storedPort(t, a, "a", 20002, "udp"); p.Enabled || p.Target != 20002 {
					t.Fatalf("append activated a UDP mapping: %+v", p)
				}
			}
			if portCount(t, a, "a") != wantCount {
				t.Fatal("append changed the instance's protocol allocation choice")
			}
			if err := a.EditPort("a", 20001, "tcp", 8080); err != nil {
				t.Fatal(err)
			}
			if p := storedPort(t, a, "a", 20001, "tcp"); p.Enabled || p.Target != 8080 {
				t.Fatalf("target-only edit exposed a service: %+v", p)
			}
			if len(b.forwards["192.0.2.1"].Ports) != 1 {
				t.Fatal("reservation-only mutations added forward rules")
			}
			enabled := true
			if err := a.UpdatePort("a", 20001, "tcp", nil, &enabled); err != nil {
				t.Fatal(err)
			}
			actual := b.forwards["192.0.2.1"].Ports
			if len(actual) != 2 || actual[1].ListenPort != "20001" || actual[1].TargetPort != "8080" {
				t.Fatalf("activation lost the configured target: %+v", actual)
			}
			enabled = false
			if err := a.UpdatePort("a", 20001, "tcp", nil, &enabled); err != nil {
				t.Fatal(err)
			}
			if p := storedPort(t, a, "a", 20001, "tcp"); p.Enabled || p.Target != 8080 || len(b.forwards["192.0.2.1"].Ports) != 1 {
				t.Fatalf("disable lost the reservation or retained the rule: %+v", p)
			}
			enabled = true
			target := 5353
			err := a.UpdatePort("a", 20001, "udp", &target, &enabled)
			if udp && err != nil {
				t.Fatal(err)
			}
			if !udp && err == nil {
				t.Fatal("enabled UDP without an allocated UDP reservation")
			}
			if udp {
				actual = b.forwards["192.0.2.1"].Ports
				if len(actual) != 2 || actual[1].Protocol != "udp" || actual[1].TargetPort != "5353" {
					t.Fatalf("explicit UDP activation failed: %+v", actual)
				}
			}
			next, err := a.Store.NextPorts(1, 20000, 20010)
			if err != nil || !reflect.DeepEqual(next, []int{20003}) {
				t.Fatalf("disabled reservation returned to the free pool: %v %v", next, err)
			}
		})
	}
}

func TestDisableLastMappingWithoutAddressKeepsSharedGuest(t *testing.T) {
	a, b := networkTestApp(t)
	for index, id := range []string{"a", "b"} {
		reservePortGuest(t, a, b, id, fmt.Sprintf("10.80.0.%d", 10+index), false)
		if err := a.SyncPorts(id); err != nil {
			t.Fatal(err)
		}
	}
	owner, _ := a.forwardOwner()
	wantOther := taggedPorts(b.forwards["192.0.2.1"].Ports, forwardTag(owner, "b"))
	b.addresses["p-a"] = ""
	a.hostPorts = func() (map[int]bool, error) { return nil, errors.New("host probe unavailable") }
	var cleanup []Port
	a.clearNAT = func(ports []Port) error { cleanup = append(cleanup, ports...); return nil }
	enabled := false
	if err := a.UpdatePort("a", 20000, "tcp", nil, &enabled); err != nil {
		t.Fatal(err)
	}
	actual := b.forwards["192.0.2.1"].Ports
	if len(taggedPorts(actual, forwardTag(owner, "a"))) != 0 || !reflect.DeepEqual(taggedPorts(actual, forwardTag(owner, "b")), wantOther) {
		t.Fatalf("disabling the last rule changed shared forwarding: %+v", actual)
	}
	if len(cleanup) != 1 || cleanup[0].Number != 20000 || cleanup[0].Proto != "tcp" || cleanup[0].replyAddress != "10.80.0.10" || cleanup[0].Target != 22 || cleanup[0].directBinding {
		t.Fatalf("disable did not drain only the old SSH binding: %+v", cleanup)
	}
	n, err := a.networkRecord("a")
	if err != nil || n.Status != "ready" || n.Target != "" || portCount(t, a, "a") != 2 {
		t.Fatalf("last disable lost its reservation or remained pending: %+v %v", n, err)
	}
}

func TestFailedDisablePersistsIntentAndCleanupUntilExplicitRetry(t *testing.T) {
	for _, failure := range []string{"write", "cleanup"} {
		t.Run(failure, func(t *testing.T) {
			a, b := networkTestApp(t)
			reservePortGuest(t, a, b, "a", "10.80.0.10", false)
			if err := a.SyncPorts("a"); err != nil {
				t.Fatal(err)
			}
			if failure == "write" {
				b.writeError = &incusx.StatusError{Code: 500, Message: "rejected disable"}
			} else {
				a.clearNAT = func([]Port) error { return errors.New("cleanup unavailable") }
			}
			enabled := false
			if err := a.UpdatePort("a", 20000, "tcp", nil, &enabled); err == nil {
				t.Fatal("failed disable was reported as applied")
			}
			n, _ := a.networkRecord("a")
			if n.Status != "pending" || storedPort(t, a, "a", 20000, "tcp").Enabled || portCount(t, a, "a") != 2 {
				t.Fatalf("failed disable lost durable intent: %+v", n)
			}
			enabled = true
			if err := a.UpdatePort("a", 20000, "tcp", nil, &enabled); err == nil {
				t.Fatal("pending disable was overwritten by another edit")
			}
			queued, err := a.conntrackCleanupPorts("a", "192.0.2.1")
			if err != nil || len(queued) != 1 || queued[0].replyAddress != "10.80.0.10" || queued[0].Target != 22 {
				t.Fatalf("old SSH tuple was not retained: %+v %v", queued, err)
			}
			reopened, err := store.Open(a.Cfg.StateDB())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			restarted := &App{Cfg: a.Cfg, Store: reopened, Incus: b, hostPorts: a.hostPorts, clearNAT: func(ports []Port) error {
				if !reflect.DeepEqual(ports, queued) {
					t.Errorf("retry changed the cleanup scope: %+v", ports)
				}
				return nil
			}}
			b.writeError = nil
			if err := restarted.SyncPorts("a"); err != nil {
				t.Fatal(err)
			}
			remaining, _ := restarted.conntrackCleanupPorts("a", "")
			n, _ = restarted.networkRecord("a")
			if len(b.forwards["192.0.2.1"].Ports) != 0 || len(remaining) != 0 || n.Status != "ready" {
				t.Fatalf("retry left rules or pending cleanup: %+v %+v", n, remaining)
			}
		})
	}
}

func TestDisabledReservationsDoNotBlockOnHostListeners(t *testing.T) {
	a, b := networkTestApp(t)
	reservePortGuest(t, a, b, "a", "10.80.0.10", true)
	a.hostPorts = func() (map[int]bool, error) { return map[int]bool{20001: true}, nil }
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	enabled := true
	if err := a.UpdatePort("a", 20001, "tcp", nil, &enabled); err == nil {
		t.Fatal("activation hid a host service")
	}
	n, _ := a.networkRecord("a")
	if n.Status != "pending" || len(b.forwards["192.0.2.1"].Ports) != 1 || portCount(t, a, "a") != 4 {
		t.Fatalf("conflict changed applied rules or released reservations: %+v", n)
	}
	a.hostPorts = func() (map[int]bool, error) { return map[int]bool{}, nil }
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	if len(b.forwards["192.0.2.1"].Ports) != 2 {
		t.Fatal("explicit retry did not apply the saved activation")
	}
}

func TestStoppedGuestCleansRuleLeftBehindByFailedDisable(t *testing.T) {
	a, b := networkTestApp(t)
	reservePortGuest(t, a, b, "a", "10.80.0.10", false)
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	b.writeError = &incusx.StatusError{Code: 500, Message: "rejected disable"}
	enabled := false
	if err := a.UpdatePort("a", 20000, "tcp", nil, &enabled); err == nil {
		t.Fatal("failed disable was hidden")
	}
	// The guest can stop externally while the disabled intent is still pending.
	// Cleanup must inspect the actual old rule instead of trusting enabled rows.
	b.writeError = nil
	b.stopped["p-a"] = true
	var cleaned int
	a.clearNAT = func(ports []Port) error {
		if len(b.forwards["192.0.2.1"].Ports) != 0 {
			t.Error("drained old connections while the stopped guest still had DNAT rules")
		}
		for _, p := range ports {
			if p.Number != 20000 || p.Proto != "tcp" || p.directBinding {
				t.Errorf("cleanup touched an unused reservation: %+v", p)
			}
		}
		cleaned += len(ports)
		return nil
	}
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	n, _ := a.networkRecord("a")
	queued, _ := a.conntrackCleanupPorts("a", "")
	if cleaned == 0 || len(queued) != 0 || len(b.forwards["192.0.2.1"].Ports) != 0 || n.Status != "inactive" || storedPort(t, a, "a", 20000, "tcp").Enabled {
		t.Fatalf("stopped guest retained a disabled rule or pending cleanup: %+v %+v", n, queued)
	}
}

func TestUncertainDisableBlocksSharedEditsWithoutChangingLedger(t *testing.T) {
	a, b := networkTestApp(t)
	for index, id := range []string{"a", "b"} {
		reservePortGuest(t, a, b, id, fmt.Sprintf("10.80.0.%d", 10+index), false)
		if err := a.SyncPorts(id); err != nil {
			t.Fatal(err)
		}
	}
	b.writeError = &incusx.ForwardUncertainError{Cause: errors.New("disable response lost")}
	enabled := false
	if err := a.UpdatePort("a", 20000, "tcp", nil, &enabled); err == nil {
		t.Fatal("uncertain disable was reported as confirmed")
	}
	if err := a.UpdatePort("b", 20002, "tcp", nil, &enabled); err == nil {
		t.Fatal("shared instance ignored the unfinished write")
	}
	if !storedPort(t, a, "b", 20002, "tcp").Enabled {
		t.Fatal("blocked shared edit changed the desired mapping")
	}
	n, _ := a.networkRecord("a")
	if !n.Uncertain || n.Status != "needs-reconciliation" {
		t.Fatalf("uncertain disable lost its barrier: %+v", n)
	}
	if err := a.SyncPorts("a"); err == nil {
		t.Fatal("uncertain disable was blindly retried")
	}
}

func TestPortActivationValidatesSavedTarget(t *testing.T) {
	a, b := networkTestApp(t)
	reservePortGuest(t, a, b, "a", "10.80.0.10", false)
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.DB.Exec(`UPDATE ports SET target=0 WHERE number=20001`); err != nil {
		t.Fatal(err)
	}
	enabled := true
	if err := a.UpdatePort("a", 20001, "tcp", nil, &enabled); err == nil {
		t.Fatal("enabled an invalid saved target")
	}
	if storedPort(t, a, "a", 20001, "tcp").Enabled {
		t.Fatal("rejected activation changed the ledger")
	}
	if err := a.UpdatePort("a", 20001, "tcp", nil, nil); err == nil {
		t.Fatal("accepted an empty mapping edit")
	}
	target := 8443
	if err := a.UpdatePort("a", 20001, "tcp", &target, &enabled); err != nil {
		t.Fatal(err)
	}
}

func TestNoEnabledMappingsDoNotCreateAnEmptyForward(t *testing.T) {
	a, b := networkTestApp(t)
	reservePortGuest(t, a, b, "a", "", false)
	if _, err := a.Store.DB.Exec(`UPDATE ports SET enabled=0 WHERE instance_id='a'`); err != nil {
		t.Fatal(err)
	}
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	if len(b.forwards) != 0 || b.writes != 0 {
		t.Fatal("created a forward object for unbound reservations")
	}
}

func TestUDPOptionPreservesLegacyCreateIdempotency(t *testing.T) {
	a, _ := testApp(t)
	if err := a.Store.SetJSONSetting("network_pool", PoolSettings{NATIPv4: "192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	legacy := json.RawMessage(`{"name":"old","image":"alpine/3.21/cloud","cpuCores":0.5,"cpuPin":"","memoryMib":128,"diskGib":1,"bandwidthMbps":0,"stackMode":"v4","sshPubKey":"","password":"","passwordLogin":null,"count":1}`)
	if _, err := a.Store.DB.Exec(`INSERT INTO tasks(id,kind,idempotency_key,request_hash,status,created_at,updated_at) VALUES('old-task','create','old-key',?,'ok',0,0)`, hashReq(legacy)); err != nil {
		t.Fatal(err)
	}
	req := CreateReq{Name: "old", Image: "alpine/3.21/cloud", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4", Count: 1}
	task, err := a.SubmitCreate(req, "old-key")
	if err != nil || task.ID != "old-task" {
		t.Fatalf("default UDP option invalidated an old idempotency key: %+v %v", task, err)
	}
	req.AllocateUDP = true
	if _, err := a.SubmitCreate(req, "old-key"); err == nil {
		t.Fatal("changed UDP choice reused a previous request's idempotency key")
	}
}

func TestInstanceReadsKeepPortLedgerAndNetworkStatusInOneSnapshot(t *testing.T) {
	for _, detail := range []bool{false, true} {
		t.Run(fmt.Sprintf("detail-%t", detail), func(t *testing.T) {
			a, b := networkTestApp(t)
			reservePortGuest(t, a, b, "a", "10.80.0.10", false)
			if err := a.SyncPorts("a"); err != nil {
				t.Fatal(err)
			}
			b.writeError = &incusx.StatusError{Code: 500, Message: "rejected disable"}
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			b.stateHook = func(string) {
				if calls.Add(1) == 1 {
					close(entered)
					<-release
				}
			}
			read := func() (Instance, error) {
				if detail {
					in, _, err := a.GetInstance("a")
					return in, err
				}
				list, err := a.ListInstances()
				if err != nil {
					return Instance{}, err
				}
				if len(list) != 1 {
					return Instance{}, fmt.Errorf("expected one instance, got %d", len(list))
				}
				return list[0], nil
			}
			type result struct {
				instance Instance
				err      error
			}
			readDone := make(chan result, 1)
			go func() {
				in, err := read()
				readDone <- result{instance: in, err: err}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("instance read never reached the backend barrier")
			}
			// A slow Incus call must not hold a database connection/transaction.
			if inUse := a.Store.DB.Stats().InUse; inUse != 0 {
				t.Errorf("instance read retained %d database connections during GetState", inUse)
			}
			mutationDone := make(chan error, 1)
			go func() {
				enabled := false
				mutationDone <- a.UpdatePort("a", 20000, "tcp", nil, &enabled)
			}()
			select {
			case err := <-mutationDone:
				if err == nil {
					t.Fatal("injected disable failure was ignored")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("slow instance read blocked a port mutation")
			}
			if len(b.forwards["192.0.2.1"].Ports) != 1 {
				t.Fatal("the injected failure unexpectedly removed the old DNAT rule")
			}
			unblock()
			var got result
			select {
			case got = <-readDone:
			case <-time.After(5 * time.Second):
				t.Fatal("instance read did not complete after releasing the backend")
			}
			if got.err != nil || got.instance.NetworkStatus != "ready" || len(got.instance.Ports) != 2 || !got.instance.Ports[0].Enabled {
				t.Fatalf("mixed pre-edit status with post-edit ports: %+v %v", got.instance, got.err)
			}
			latest, err := read()
			if err != nil || latest.NetworkStatus != "pending" || latest.NetworkError == "" || len(latest.Ports) != 2 || latest.Ports[0].Enabled {
				t.Fatalf("next read did not expose the pending disabled intent: %+v %v", latest, err)
			}
		})
	}
}
