package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"particeps/internal/incusx"
	"particeps/internal/store"
)

type forwardBackend struct {
	stateHook func(string)
	*resourceBackend
	forwards                  map[string]incusx.Forward
	versions                  map[string]int
	addresses                 map[string]string
	stopped, deleted          map[string]bool
	writeError, deleteError   error
	stateError, readbackError error
	ignoreWrite, ignoreDelete bool
	writes, deletes           int
	beforeUpdate              func()
	beforePower               func()
	powerError                error
	events                    []string
}

func cloneForward(f incusx.Forward) incusx.Forward {
	data, _ := json.Marshal(f)
	var result incusx.Forward
	_ = json.Unmarshal(data, &result)
	return result
}

func (b *forwardBackend) ListForwards(string) ([]incusx.Forward, error) {
	var result []incusx.Forward
	for _, f := range b.forwards {
		result = append(result, cloneForward(f))
	}
	return result, nil
}

func (b *forwardBackend) GetForward(_, listen string) (incusx.Forward, string, error) {
	if b.writes > 0 && b.readbackError != nil {
		return incusx.Forward{}, "", b.readbackError
	}
	f, ok := b.forwards[listen]
	if !ok {
		return f, "", &incusx.StatusError{Code: http.StatusNotFound, Message: "forward absent"}
	}
	return cloneForward(f), fmt.Sprintf(`"%d"`, b.versions[listen]), nil
}

func (b *forwardBackend) CreateForward(_ string, f incusx.Forward) error {
	if b.writeError != nil {
		return b.writeError
	}
	if _, ok := b.forwards[f.ListenAddress]; ok {
		return &incusx.StatusError{Code: http.StatusConflict}
	}
	b.writes++
	if !b.ignoreWrite {
		b.forwards[f.ListenAddress] = cloneForward(f)
		b.versions[f.ListenAddress]++
	}
	b.events = append(b.events, "forward")
	return nil
}

func (b *forwardBackend) UpdateForward(_ string, f incusx.Forward, etag string) error {
	if b.beforeUpdate != nil {
		hook := b.beforeUpdate
		b.beforeUpdate = nil
		hook()
	}
	if etag != fmt.Sprintf(`"%d"`, b.versions[f.ListenAddress]) {
		return &incusx.StatusError{Code: http.StatusPreconditionFailed}
	}
	if b.writeError != nil {
		return b.writeError
	}
	b.writes++
	if !b.ignoreWrite {
		b.forwards[f.ListenAddress] = cloneForward(f)
		b.versions[f.ListenAddress]++
	}
	b.events = append(b.events, "forward")
	return nil
}

func testNetworkState(ip, status string) *incusx.InstanceState {
	data, _ := json.Marshal(map[string]any{"status": status, "network": map[string]any{
		"eth0": map[string]any{"addresses": []map[string]string{{"family": "inet", "address": ip}}}}})
	var state incusx.InstanceState
	_ = json.Unmarshal(data, &state)
	return &state
}

func (b *forwardBackend) GetState(name string) (*incusx.InstanceState, error) {
	if b.stateHook != nil {
		b.stateHook(name)
	}
	if b.stateError != nil {
		return nil, b.stateError
	}
	if b.deleted[name] {
		return nil, &incusx.StatusError{Code: http.StatusNotFound}
	}
	if b.stopped[name] {
		return testNetworkState("", "Stopped"), nil
	}
	return testNetworkState(b.addresses[name], "Running"), nil
}

func (b *forwardBackend) GetConfig(name string) (map[string]any, error) {
	if b.deleted[name] {
		return nil, &incusx.StatusError{Code: http.StatusNotFound}
	}
	return b.resourceBackend.GetConfig(name)
}

func (b *forwardBackend) SetState(name, action string, _ bool) error {
	if b.beforePower != nil {
		b.beforePower()
	}
	if b.powerError != nil {
		return b.powerError
	}
	b.stopped[name] = action == "stop"
	b.events = append(b.events, action)
	return nil
}

func (b *forwardBackend) DeleteInstance(name string) error {
	b.deletes++
	if b.deleteError != nil {
		return b.deleteError
	}
	if !b.ignoreDelete {
		b.deleted[name] = true
	}
	b.events = append(b.events, "delete")
	return nil
}

func networkTestApp(t *testing.T) (*App, *forwardBackend) {
	t.Helper()
	a, base := testApp(t)
	if _, err := a.Store.DB.Exec(`DELETE FROM instances`); err != nil {
		t.Fatal(err)
	}
	a.Cfg.PortsPerGuest = 2
	b := &forwardBackend{resourceBackend: base, forwards: map[string]incusx.Forward{}, versions: map[string]int{},
		addresses: map[string]string{}, stopped: map[string]bool{}, deleted: map[string]bool{}}
	a.Incus = b
	return a, b
}

func reserveNetworkGuest(t *testing.T, a *App, b *forwardBackend, id, address string) {
	t.Helper()
	b.addresses["p-"+id] = address
	err := a.reserveInstance(id, id, "p-"+id, CreateReq{Image: "alpine", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4", AllocateUDP: true}, true, "192.0.2.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Exercise upgraded instances whose original TCP and UDP rules all remain
	// enabled. The new reservation defaults have their own coverage in ports_test.
	if _, err := a.Store.DB.Exec(`UPDATE ports SET enabled=1 WHERE instance_id=?`, id); err != nil {
		t.Fatal(err)
	}
}

func portCount(t *testing.T, a *App, id string) int {
	t.Helper()
	var count int
	if err := a.Store.DB.QueryRow(`SELECT COUNT(*) FROM ports WHERE instance_id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestSharedForwardConcurrentCreateAndDeleteKeepsOtherGuest(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	reserveNetworkGuest(t, a, b, "b", "10.80.0.11")
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := a.SyncPorts(id); err != nil {
				t.Error(err)
			}
		}(id)
	}
	wg.Wait()
	if len(b.forwards) != 1 || len(b.forwards["192.0.2.1"].Ports) != 8 {
		t.Fatalf("shared forward lost a guest: %+v", b.forwards)
	}
	owner, _ := a.forwardOwner()
	wantB := taggedPorts(b.forwards["192.0.2.1"].Ports, forwardTag(owner, "b"))
	b.events = nil
	if err := a.DeleteInstance("a"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wantB, taggedPorts(b.forwards["192.0.2.1"].Ports, forwardTag(owner, "b"))) || portCount(t, a, "b") != 4 {
		t.Fatal("deleting A changed B's rules or reservations")
	}
	if !reflect.DeepEqual(b.events, []string{"forward", "stop", "delete"}) || portCount(t, a, "a") != 0 {
		t.Fatalf("cleanup/release order is unsafe: %v", b.events)
	}
	if err := a.DeleteInstance("b"); err != nil {
		t.Fatal(err)
	}
	if len(b.forwards["192.0.2.1"].Ports) != 0 {
		t.Fatal("last guest's rules were not removed")
	}
	ports, err := a.Store.NextPorts(2, 20000, 20003)
	if err != nil || !reflect.DeepEqual(ports, []int{20000, 20001}) {
		t.Fatalf("cleaned ports were not released: %v %v", ports, err)
	}
}

func TestForwardCleanupFailureKeepsGuestAndReservations(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	b.writeError = &incusx.StatusError{Code: 500, Message: "firewall update failed"}
	if err := a.DeleteInstance("a"); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	n, _ := a.networkRecord("a")
	if portCount(t, a, "a") != 4 || b.deletes != 0 || !n.Deleting || n.Status != "cleanup-pending" {
		t.Fatalf("unsafe release after cleanup failure: %+v", n)
	}
	if err := a.SyncPorts("a"); err == nil {
		t.Fatal("deleting guest accepted new forwarding")
	}
	reserveNetworkGuest(t, a, b, "b", "10.80.0.11")
	ports, _ := a.instancePorts("b")
	if ports[0].Number != 20002 {
		t.Fatal("pending cleanup port was reallocated")
	}
	b.writeError = nil
	if err := a.DeleteInstance("a"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteFailureAndFalseSuccessNeverReleasePorts(t *testing.T) {
	for _, mode := range []string{"error", "false-success", "text-not-found"} {
		t.Run(mode, func(t *testing.T) {
			a, b := networkTestApp(t)
			reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
			if err := a.SyncPorts("a"); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "error":
				b.deleteError = errors.New("delete operation timed out")
			case "false-success":
				b.ignoreDelete = true
			case "text-not-found":
				b.stateError = errors.New("connection lost: not found in cache")
			}
			if err := a.DeleteInstance("a"); err == nil || portCount(t, a, "a") != 4 {
				t.Fatalf("unconfirmed deletion released ports: %v", err)
			}
			b.deleteError, b.stateError, b.ignoreDelete = nil, nil, false
			if err := a.DeleteInstance("a"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteDatabaseFailureRollsBackPortRelease(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.DB.Exec(`CREATE TRIGGER reject_delete BEFORE DELETE ON instances BEGIN SELECT RAISE(ABORT,'injected DB failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteInstance("a"); err == nil || portCount(t, a, "a") != 4 {
		t.Fatalf("partial DB release: %v", err)
	}
	if _, err := a.Store.DB.Exec(`DROP TRIGGER reject_delete`); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteInstance("a"); err != nil {
		t.Fatal(err)
	}
}

func TestAllocationSkipsHostAndExternalRangesAndRejectsManualConflict(t *testing.T) {
	a, b := networkTestApp(t)
	a.hostPorts = func() (map[int]bool, error) { return map[int]bool{20000: true}, nil }
	foreign := incusx.ForwardPort{Description: "external owner", Protocol: "udp", ListenPort: "20001-20003", TargetPort: "53", TargetAddress: "10.80.0.99"}
	b.forwards["192.0.2.2"] = incusx.Forward{ListenAddress: "192.0.2.2", Description: "external forward", Config: map[string]string{"user.note": "keep"}, Ports: []incusx.ForwardPort{foreign}}
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	ports, _ := a.instancePorts("a")
	if ports[0].Number != 20004 {
		t.Fatalf("did not skip occupied range: %+v", ports)
	}
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	for _, number := range []int{20000, 20002, 20004} {
		if err := a.AddPort("a", number); err == nil {
			t.Fatalf("accepted occupied port %d", number)
		}
	}
	if err := a.DeleteInstance("a"); err != nil {
		t.Fatal(err)
	}
	f := b.forwards["192.0.2.2"]
	if !reflect.DeepEqual(f.Ports, []incusx.ForwardPort{foreign}) || f.Config["user.note"] != "keep" || f.Description != "external forward" {
		t.Fatalf("external forward was changed: %+v", f)
	}
}

func TestOwnedForwardWithExternalRulesIsNotOverwritten(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	foreign := incusx.ForwardPort{Description: "another writer", Protocol: "tcp", ListenPort: "30000", TargetPort: "80", TargetAddress: "10.80.0.99"}
	f := b.forwards["192.0.2.1"]
	f.Ports = append(f.Ports, foreign)
	b.forwards["192.0.2.1"] = f
	before := b.writes
	if err := a.EditPort("a", 20001, "tcp", 8080); err == nil {
		t.Fatal("modified an object containing external rules")
	}
	if b.writes != before || !reflect.DeepEqual(b.forwards["192.0.2.1"], f) {
		t.Fatal("external modification was overwritten")
	}
}

func TestAppendEditAndAddressRefreshKeepOwnershipAndTargets(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	if err := a.AddPort("a", 0); err != nil {
		t.Fatal(err)
	}
	if err := a.EditPort("a", 20002, "tcp", 8080); err != nil {
		t.Fatal(err)
	}
	enabled := true
	if err := a.UpdatePort("a", 20002, "tcp", nil, &enabled); err != nil {
		t.Fatal(err)
	}
	if err := a.Power("a", "stop", false); err != nil {
		t.Fatal(err)
	}
	b.addresses["p-a"] = "10.80.0.12"
	if err := a.Power("a", "start", false); err != nil {
		t.Fatal(err)
	}
	b.addresses["p-a"] = "10.80.0.13"
	a.refreshForwardAddress("a", testNetworkState("10.80.0.13", "Running"))
	for _, p := range b.forwards["192.0.2.1"].Ports {
		want := p.ListenPort
		if p.Protocol == "tcp" && p.ListenPort == "20000" {
			want = "22"
		}
		if p.Protocol == "tcp" && p.ListenPort == "20002" {
			want = "8080"
		}
		if p.TargetAddress != "10.80.0.13" || p.TargetPort != want {
			t.Fatalf("binding or custom target changed incorrectly: %+v", p)
		}
	}
	if portCount(t, a, "a") != 6 {
		t.Fatal("protocols were not allocated together")
	}
}

func TestFailedPortEditIsDurableAndExplicitRetryAppliesIt(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	b.writeError = &incusx.StatusError{Code: 500, Message: "rejected update"}
	if err := a.EditPort("a", 20001, "tcp", 8080); err == nil {
		t.Fatal("failed edit was reported as applied")
	}
	if err := a.EditPort("a", 20001, "tcp", 9000); err == nil {
		t.Fatal("pending edit was overwritten")
	}
	reopened, err := store.Open(a.Cfg.StateDB())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restarted := &App{Cfg: a.Cfg, Store: reopened, Incus: b, clearNAT: a.clearNAT}
	n, err := restarted.networkRecord("a")
	if err != nil || n.Status != "pending" {
		t.Fatalf("pending state was lost: %+v %v", n, err)
	}
	b.writeError = nil
	if err := restarted.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	for _, p := range b.forwards["192.0.2.1"].Ports {
		if p.ListenPort == "20001" && p.Protocol == "tcp" && p.TargetPort != "8080" {
			t.Fatal("retry lost saved target")
		}
	}
}

func TestUnknownForwardWriteCannotBeRetriedOrDeleted(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	b.writeError = &incusx.ForwardUncertainError{Cause: errors.New("response lost while server is still writing")}
	if err := a.SyncPorts("a"); err == nil {
		t.Fatal("unknown write reported success")
	}
	b.writeError = nil
	if err := a.SyncPorts("a"); err == nil {
		t.Fatal("blindly retried an uncertain write")
	}
	if err := a.DeleteInstance("a"); err == nil {
		t.Fatal("deleted during an uncertain write")
	}
	n, _ := a.networkRecord("a")
	if !n.Uncertain || n.Status != "needs-reconciliation" || portCount(t, a, "a") != 4 || b.deletes != 0 {
		t.Fatalf("unknown write lost its durable protection: %+v", n)
	}
}

func TestForwardFalseSuccessOrFailedReadbackStaysPending(t *testing.T) {
	for _, mode := range []string{"ignored", "readback-error"} {
		t.Run(mode, func(t *testing.T) {
			a, b := networkTestApp(t)
			reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
			b.ignoreWrite = mode == "ignored"
			if mode == "readback-error" {
				b.readbackError = errors.New("backend unavailable")
			}
			if err := a.SyncPorts("a"); err == nil {
				t.Fatal("unverified forward was reported ready")
			}
			n, _ := a.networkRecord("a")
			if n.Status != "pending" || portCount(t, a, "a") != 4 {
				t.Fatalf("lost pending reservation: %+v", n)
			}
		})
	}
}

func TestReadHostPortsSeesRealTCPAndUDPBindings(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	used, err := readHostPorts()
	if err != nil {
		t.Fatal(err)
	}
	if !used[tcp.Addr().(*net.TCPAddr).Port] || !used[udp.LocalAddr().(*net.UDPAddr).Port] {
		t.Fatal("host port conflict check missed a real binding")
	}
}

func TestForwardJournalSurvivesCrashAndBlocksEverySharedGuest(t *testing.T) {
	a, b := networkTestApp(t)
	for i, id := range []string{"a", "b"} {
		reserveNetworkGuest(t, a, b, id, fmt.Sprintf("10.80.0.%d", 10+i))
		if err := a.SyncPorts(id); err != nil {
			t.Fatal(err)
		}
	}
	// Model a process exit after the write-ahead record commits but before any
	// HTTP result is recorded. No per-instance uncertain flag has been written.
	if _, err := a.Store.DB.Exec(`INSERT INTO forward_writes(network,listen_address,token,instance_id,phase,created_at) VALUES(?,'192.0.2.1','interrupted','a','writing',0)`, a.Cfg.Network); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(a.Cfg.StateDB())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restarted := &App{Cfg: a.Cfg, Store: reopened, Incus: b, clearNAT: a.clearNAT}
	before := b.writes
	if err := restarted.SyncPorts("b"); err == nil {
		t.Fatal("B wrote over A's interrupted whole-address request")
	}
	if err := restarted.DeleteInstance("b"); err == nil {
		t.Fatal("B deleted during A's unfinished request")
	}
	if b.writes != before || b.deletes != 0 || portCount(t, a, "a") != 4 || portCount(t, a, "b") != 4 {
		t.Fatal("unfinished shared write did not preserve every reservation")
	}
	if _, err := a.occupiedPortsLocked("192.0.2.1"); err == nil {
		t.Fatal("allocated through a blocked shared address")
	}
}

func TestForwardJournalIsCommittedBeforeBackendWrite(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	b.beforeUpdate = func() {
		var phase string
		if err := a.Store.DB.QueryRow(`SELECT phase FROM forward_writes WHERE listen_address='192.0.2.1'`).Scan(&phase); err != nil || phase != "writing" {
			t.Errorf("write was sent before journal commit: %q %v", phase, err)
		}
	}
	if err := a.EditPort("a", 20001, "tcp", 8080); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := a.Store.DB.QueryRow(`SELECT COUNT(*) FROM forward_writes`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("confirmed write retained a barrier: %d %v", remaining, err)
	}
}

func TestExternalForwardOnSelectedAddressIsRejected(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	foreign := incusx.Forward{ListenAddress: "192.0.2.1", Description: "external", Config: map[string]string{}, Ports: []incusx.ForwardPort{}}
	b.forwards["192.0.2.1"] = foreign
	if err := a.SyncPorts("a"); err == nil {
		t.Fatal("adopted an unowned address object")
	}
	if b.writes != 0 || !reflect.DeepEqual(b.forwards["192.0.2.1"], foreign) {
		t.Fatal("external address object changed")
	}
}

func TestHostListenerAppearingAfterReservationBlocksApply(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	a.hostPorts = func() (map[int]bool, error) { return map[int]bool{20000: true}, nil }
	if err := a.SyncPorts("a"); err == nil {
		t.Fatal("late host listener was hidden by DNAT")
	}
	n, _ := a.networkRecord("a")
	if b.writes != 0 || n.Status != "pending" || portCount(t, a, "a") != 4 {
		t.Fatal("late conflict lost pending reservation")
	}
}

func TestStoppedGuestDoesNotForwardToReusedDHCPAddress(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	if err := a.Power("a", "stop", false); err != nil {
		t.Fatal(err)
	}
	reserveNetworkGuest(t, a, b, "b", "10.80.0.10")
	if err := a.SyncPorts("b"); err != nil {
		t.Fatal(err)
	}
	if err := a.EditPort("a", 20001, "tcp", 8080); err != nil {
		t.Fatal(err)
	}
	owner, _ := a.forwardOwner()
	if len(taggedPorts(b.forwards["192.0.2.1"].Ports, forwardTag(owner, "a"))) != 0 || portCount(t, a, "a") != 4 {
		t.Fatal("stopped guest's stale address remained reachable")
	}
	n, _ := a.networkRecord("a")
	if n.Status != "inactive" {
		t.Fatalf("stopped mapping status=%s", n.Status)
	}
}

func TestConntrackCleanupFailureKeepsInstanceAndPortReservations(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	a.clearNAT = func(ports []Port) error { return errors.New("conntrack cleanup denied") }
	if err := a.DeleteInstance("a"); err == nil {
		t.Fatal("ignored connection cleanup failure")
	}
	if b.deletes != 0 || portCount(t, a, "a") != 4 {
		t.Fatal("released address before old NAT sessions were removed")
	}
	var pending int
	if err := a.Store.DB.QueryRow(`SELECT COUNT(*) FROM conntrack_cleanup WHERE instance_id='a'`).Scan(&pending); err != nil || pending != 4 {
		t.Fatalf("cleanup intent was not retained: %d %v", pending, err)
	}
	a.clearNAT = func([]Port) error { return nil }
	if err := a.DeleteInstance("a"); err != nil {
		t.Fatal(err)
	}
}

func TestEditedTargetRetainsPreciseConntrackCleanupAcrossRetry(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	var received []Port
	a.clearNAT = func(ports []Port) error { received = ports; return errors.New("cleanup interrupted") }
	if err := a.EditPort("a", 20001, "tcp", 8080); err == nil {
		t.Fatal("failed session cleanup reported applied")
	}
	var oldBindings []Port
	for _, p := range received {
		if !p.directBinding {
			oldBindings = append(oldBindings, p)
		}
	}
	if len(oldBindings) != 1 || oldBindings[0].Number != 20001 || oldBindings[0].Proto != "tcp" {
		t.Fatalf("cleanup touched unrelated NAT bindings: %+v", received)
	}
	a.clearNAT = func(ports []Port) error {
		if !reflect.DeepEqual(ports, received) {
			t.Errorf("retry lost queued cleanup: %+v", ports)
		}
		return nil
	}
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
}

func TestIPv4HostPortProbeAllowsDisabledIPv6(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"tcp", "udp"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("  sl local_address rem_address st\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readHostPortsAt(root); err != nil {
		t.Fatalf("missing optional IPv6 socket files blocked IPv4: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "tcp")); err != nil {
		t.Fatal(err)
	}
	if _, err := readHostPortsAt(root); err == nil {
		t.Fatal("missing required IPv4 table was ignored")
	}
}

func TestUnimplementedRebuildDoesNotStopOrChangeForwarding(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	before := cloneForward(b.forwards["192.0.2.1"])
	b.events = nil
	if err := a.Rebuild("a", "debian/13/cloud"); err == nil {
		t.Fatal("unimplemented rebuild accepted")
	}
	if len(b.events) != 0 || !reflect.DeepEqual(before, b.forwards["192.0.2.1"]) {
		t.Fatal("unsupported rebuild changed the guest before returning an error")
	}
}

func TestUnfinishedPowerOperationCannotReinstallForwarding(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	b.beforePower = func() {
		n, err := a.networkRecord("a")
		if err != nil || !n.Uncertain {
			t.Error("power request dispatched before durable protection")
		}
	}
	b.powerError = errors.New("stop wait timed out while instance still reports Running")
	if err := a.Power("a", "stop", false); err == nil {
		t.Fatal("unconfirmed stop reported success")
	}
	before := b.writes
	if err := a.SyncPorts("a"); err == nil || b.writes != before {
		t.Fatal("DNAT reinstalled before late stop completed")
	}
	n, _ := a.networkRecord("a")
	if !n.Uncertain || n.Status != "needs-reconciliation" || len(b.forwards["192.0.2.1"].Ports) != 0 {
		t.Fatalf("late power operation is not protected: %+v", n)
	}
}

func TestObservedAddressLossDisablesAndRestoredAddressResumesForwarding(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	b.addresses["p-a"] = ""
	a.refreshForwardAddress("a", testNetworkState("", "Running"))
	n, _ := a.networkRecord("a")
	if n.Status != "waiting-address" || len(b.forwards["192.0.2.1"].Ports) != 0 {
		t.Fatalf("lost address kept stale forwarding: %+v", n)
	}
	b.addresses["p-a"] = "10.80.0.12"
	a.refreshForwardAddress("a", testNetworkState("10.80.0.12", "Running"))
	n, _ = a.networkRecord("a")
	if n.Status != "ready" || n.Target != "10.80.0.12" || len(b.forwards["192.0.2.1"].Ports) != 4 {
		t.Fatalf("restored address did not resume: %+v", n)
	}
	b.stopped["p-a"] = true
	a.refreshForwardAddress("a", testNetworkState("", "Stopped"))
	b.stopped["p-a"] = false
	a.refreshForwardAddress("a", testNetworkState("10.80.0.12", "Running"))
	n, _ = a.networkRecord("a")
	if n.Status != "ready" || len(b.forwards["192.0.2.1"].Ports) != 4 {
		t.Fatal("known inactive state never resumed after observed start")
	}
}

func TestConntrackDrainTargetsOnlyThePreviousBinding(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	a.clearNAT = func(ports []Port) error {
		for _, pending := range ports {
			for _, live := range b.forwards[pending.ListenIP].Ports {
				if live.ListenPort == fmt.Sprint(pending.Number) && live.Protocol == pending.Proto && live.TargetAddress == pending.replyAddress && live.TargetPort == fmt.Sprint(pending.Target) {
					t.Error("old target is still active while draining its connections")
				}
			}
		}
		return nil
	}
	before := b.writes
	if err := a.EditPort("a", 20001, "udp", 8080); err != nil {
		t.Fatal(err)
	}
	if b.writes-before != 1 {
		t.Fatal("target edit did not replace the mapping in one backend update")
	}
}

func TestSyncFinishesFailedStopCleanupBeforeRestoringRules(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "10.80.0.10")
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	a.clearNAT = func([]Port) error { return errors.New("temporary conntrack failure") }
	if err := a.Power("a", "stop", false); err == nil {
		t.Fatal("failed stop cleanup reported success")
	}
	if b.stopped["p-a"] {
		t.Fatal("guest stopped before cleanup succeeded")
	}
	a.clearNAT = func(ports []Port) error {
		for _, p := range ports {
			if !p.directBinding && len(b.forwards["192.0.2.1"].Ports) != 0 {
				t.Error("restored rules before broad cleanup completed")
			}
		}
		return nil
	}
	if err := a.SyncPorts("a"); err != nil {
		t.Fatal(err)
	}
	if len(b.forwards["192.0.2.1"].Ports) != 4 {
		t.Fatal("rules did not resume after successful cleanup")
	}
}

func TestInitialDHCPWaitLeavesOtherAllocationsUnblocked(t *testing.T) {
	a, b := networkTestApp(t)
	reserveNetworkGuest(t, a, b, "a", "")
	entered, release := make(chan struct{}), make(chan struct{})
	reads := 0
	b.stateHook = func(name string) {
		reads++
		if reads == 1 {
			close(entered)
		}
		if reads == 2 {
			<-release
			b.addresses[name] = "10.80.0.10"
		}
	}
	done := make(chan error, 1)
	go func() { done <- a.applyForwards("a", "p-a", "192.0.2.1") }()
	<-entered
	available := make(chan struct{})
	go func() { a.allocationMu.Lock(); a.allocationMu.Unlock(); close(available) }()
	select {
	case <-available:
	case <-time.After(time.Second):
		t.Error("DHCP wait held the allocation lock")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	n, _ := a.networkRecord("a")
	if n.Status != "ready" || n.Target != "10.80.0.10" {
		t.Fatalf("delayed lease was not applied: %+v", n)
	}
}

func TestHostAcceptedConnectionStillReservesPortAfterListenerCloses(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	port := accepted.LocalAddr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	used, err := readHostPorts()
	if err != nil || !used[port] {
		t.Fatalf("active host connection was treated as a free port: %d %v", port, err)
	}
}
