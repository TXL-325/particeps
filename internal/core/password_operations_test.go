package core

import (
	"errors"
	"testing"
	"time"

	"particeps/internal/incusx"
	"particeps/internal/store"
)

func TestUncertainPasswordOperationSurvivesRestartAndBlocksLifecycle(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend, failure: &incusx.ExecError{Operation: incusx.ConfigOperation{ID: "/1.0/operations/password-fixture"}, Cause: errors.New("wait timeout")}}
	a.Incus = fake
	if _, err := a.ResetPassword("guest", "first-fixture-password"); err == nil {
		t.Fatal("unknown result accepted")
	}
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(a.Cfg.StateDB())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	a = &App{Cfg: a.Cfg, Store: reopened, Metrics: a.Metrics, Incus: fake}
	var operation string
	if err := a.Store.DB.QueryRow("SELECT operation FROM password_updates WHERE instance_id='guest'").Scan(&operation); err != nil || operation != "/1.0/operations/password-fixture" {
		t.Fatal("operation reference was lost")
	}
	for _, action := range []func() error{
		func() error { _, err := a.ResetPassword("guest", "second-fixture-password"); return err },
		func() error { return a.Power("guest", "stop", false) },
		func() error { return a.DeleteInstance("guest") },
	} {
		if err := action(); err == nil {
			t.Fatal("unfinished password operation did not block mutation")
		}
	}
	if fake.calls != 1 {
		t.Fatal("password was replayed")
	}
	backend.operationDone = true
	fake.failure = nil
	if _, err := a.ResetPassword("guest", "second-fixture-password"); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 2 || fake.password != "second-fixture-password" {
		t.Fatal("confirmed completion did not allow a new reset")
	}
	var pending int
	if err := a.Store.DB.QueryRow("SELECT COUNT(*) FROM password_updates").Scan(&pending); err != nil || pending != 0 {
		t.Fatal("completed operation remains pending")
	}
}

func TestUnknownPasswordDispatchIsNotBlindlyRetried(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend}
	a.Incus = fake
	if _, err := a.Store.DB.Exec("INSERT INTO password_updates(instance_id,created_at) VALUES('guest',0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResetPassword("guest", "fixture-password"); err == nil || fake.calls != 0 {
		t.Fatal("unreferenced password operation was replayed")
	}
}

func TestKnownPasswordFailureAllowsRetry(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend, failure: &incusx.ExecError{Operation: incusx.ConfigOperation{Terminal: true}, Cause: errors.New("command exited")}}
	a.Incus = fake
	if _, err := a.ResetPassword("guest", "fixture-password"); err == nil {
		t.Fatal("failed command accepted")
	}
	fake.failure = nil
	if _, err := a.ResetPassword("guest", "replacement-password"); err != nil || fake.calls != 2 {
		t.Fatal("terminal failure blocked a valid retry")
	}
}

func TestPasswordResetRejectsUnfinishedPowerOperation(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend}
	a.Incus = fake
	_, err := a.Store.DB.Exec("INSERT INTO instance_network(instance_id,status,uncertain,updated_at) VALUES('guest','needs-reconciliation',1,0)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResetPassword("guest", "fixture-password"); err == nil || fake.calls != 0 {
		t.Fatal("running state bypassed an unfinished power operation")
	}
}

func TestInitialPasswordPathAlsoRetainsUnknownOperation(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend, failure: &incusx.ExecError{Operation: incusx.ConfigOperation{ID: "/1.0/operations/initial-password"}, Cause: errors.New("wait timeout")}}
	a.Incus = fake
	// This is the same durable dispatch used by runCreateItem after reservation.
	unlock := a.lockInstance("guest")
	err := a.setInstancePassword("guest", "p-guest", "fixture-initial-password")
	unlock()
	if err == nil {
		t.Fatal("initial password timeout reported success")
	}
	if _, err := a.ResetPassword("guest", "replacement-password"); err == nil || fake.calls != 1 {
		t.Fatal("initialization timeout allowed a later password overwrite")
	}
}

func TestPasswordJournalWriteFailurePreventsBackendDispatch(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend}
	a.Incus = fake
	_, err := a.Store.DB.Exec("CREATE TRIGGER fail_password_journal BEFORE INSERT ON password_updates BEGIN SELECT RAISE(ABORT,'injected'); END")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResetPassword("guest", "fixture-password"); err == nil || fake.calls != 0 {
		t.Fatal("password sent without a durable operation record")
	}
}

func TestWaitingPowerRequestCannotRetargetAnIDAsAnotherInstancesName(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend, power: make(chan string, 1)}
	a.Incus = fake
	unlock := a.lockInstance("guest")
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	done := make(chan error, 1)
	go func() { done <- a.Power("guest", "stop", false) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.instanceLocksMu.Lock()
		queued := a.instanceLocks["guest"].users > 1
		a.instanceLocksMu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("power request did not reach the instance lock")
		}
		time.Sleep(time.Millisecond)
	}
	// Simulate the current lock holder deleting A while B has a name equal to
	// A's former canonical ID. The waiting operation must still refer to A.
	if _, err := a.Store.DB.Exec(`DELETE FROM instances WHERE id='guest';
		INSERT INTO instances(id,name,incus_name,display_name,image,cpu_cores,memory_mib,disk_gib,stack_mode,desired_power,created_at)
		VALUES('other','guest','p-other','other','image',0.5,128,1,'v4','running',0)`); err != nil {
		t.Fatal(err)
	}
	unlock()
	unlock = nil
	if err := <-done; err == nil {
		t.Fatal("deleted ID was re-resolved as another instance's name")
	}
	if len(fake.power) != 0 {
		t.Fatal("power action reached the wrong instance")
	}
	var desired string
	if err := a.Store.DB.QueryRow(`SELECT desired_power FROM instances WHERE id='other'`).Scan(&desired); err != nil || desired != "running" {
		t.Fatal("another instance's desired state was changed")
	}
}
