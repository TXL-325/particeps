package core

import (
	"fmt"
	"sync"
	"testing"

	"particeps/internal/cgroupcap"
)

func TestConcurrentReservationsKeepWholePortBlocks(t *testing.T) {
	a, _ := testApp(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			id := fmt.Sprintf("new-%d", index)
			results <- a.reserveInstance(id, id, id, CreateReq{Image: "alpine", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4"}, true, "192.0.2.1", "", "")
		}(i)
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var rows, numbers, owners int
	if err := a.Store.DB.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT number) FROM ports`).Scan(&rows, &numbers); err != nil {
		t.Fatal(err)
	}
	if rows != 80 || numbers != 40 {
		t.Fatalf("partial allocation: rows=%d, numbers=%d", rows, numbers)
	}
	if err := a.Store.DB.QueryRow(`SELECT COUNT(*) FROM (SELECT number FROM ports GROUP BY number HAVING COUNT(DISTINCT instance_id)>1)`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 0 {
		t.Fatal("TCP and UDP of the same port have different owners")
	}
}

func TestReservationRollsBackWhenAnyPortWriteFails(t *testing.T) {
	a, _ := testApp(t)
	if _, err := a.Store.DB.Exec(`CREATE TRIGGER reject_udp BEFORE INSERT ON ports WHEN NEW.proto='udp' BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	err := a.reserveInstance("new", "new", "new", CreateReq{Image: "alpine", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4"}, true, "192.0.2.1", "", "")
	if err == nil {
		t.Fatal("port write failure was ignored")
	}
	var instances, ports int
	_ = a.Store.DB.QueryRow(`SELECT COUNT(*) FROM instances WHERE id='new'`).Scan(&instances)
	_ = a.Store.DB.QueryRow(`SELECT COUNT(*) FROM ports`).Scan(&ports)
	if instances != 0 || ports != 0 {
		t.Fatalf("incomplete rollback: instances=%d ports=%d", instances, ports)
	}
}

func TestFailedCPUCapDoesNotPublishSuccess(t *testing.T) {
	a, _ := testApp(t)
	a.applyCap = func(cores float64) cgroupcap.Status {
		return cgroupcap.Status{Cores: cores, Applied: false, Note: "permission denied"}
	}
	if err := a.SetCPUCap(1); err == nil {
		t.Fatal("failed kernel cap reported as success")
	}
	cores, _ := a.CPUCapStatus()
	if cores != 2 {
		t.Fatalf("failed cap replaced last valid value: %v", cores)
	}
	if value := a.Store.Setting("cpu_cap_cores", ""); value != "" {
		t.Fatalf("failed cap was persisted: %q", value)
	}
	_, status := a.CPUCapStatus()
	if status.Applied {
		t.Fatal("unverified restoration kept Applied=true")
	}
	err := a.reserveInstance("new", "new", "new", CreateReq{CPUCores: 0.5}, true, "192.0.2.1", "", "")
	if err == nil {
		t.Fatal("created a reservation while the cap was unverified")
	}
}

func TestFailedCPUCapRestoresAndVerifiesPreviousValue(t *testing.T) {
	a, _ := testApp(t)
	var attempts []float64
	a.applyCap = func(cores float64) cgroupcap.Status {
		attempts = append(attempts, cores)
		return cgroupcap.Status{Cores: cores, Applied: cores == 2}
	}
	if err := a.SetCPUCap(1); err == nil {
		t.Fatal("failed requested cap reported success")
	}
	_, status := a.CPUCapStatus()
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 || !status.Applied || status.Cores != 2 {
		t.Fatalf("previous cap not verified: attempts=%v status=%+v", attempts, status)
	}
}
