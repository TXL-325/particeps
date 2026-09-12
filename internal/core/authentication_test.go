package core

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type passwordBackend struct {
	*resourceBackend
	mu       sync.Mutex
	calls    int
	password string
	failure  error
	entered  chan string
	release  chan struct{}
	power    chan string
}

func (b *passwordBackend) SetRootPassword(name, password string) error {
	if b.entered != nil {
		b.entered <- name
		<-b.release
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if b.failure != nil {
		return b.failure
	}
	b.password = password
	return nil
}
func (b *passwordBackend) SetState(name, action string, force bool) error {
	if b.power != nil {
		b.power <- action
	}
	return b.resourceBackend.SetState(name, action, force)
}
func seedInitialPassword(t *testing.T, a *App) {
	t.Helper()
	sealed, err := a.sealCredential("credentials", "guest", "old-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	saveTestCredential(t, a, sealed)
}
func TestPasswordResetInvalidatesInitialCredentials(t *testing.T) {
	for _, fails := range []bool{false, true} {
		a, backend := testApp(t)
		fake := &passwordBackend{resourceBackend: backend}
		if fails {
			fake.failure = errors.New("uncertain backend result")
		}
		a.Incus = fake
		seedInitialPassword(t, a)
		_, err := a.ResetPassword("guest", "new-fixture-password")
		if (err != nil) != fails {
			t.Fatal("wrong reset outcome")
		}
		values, err := a.ClaimInitialCredentials("credentials")
		if err != nil || len(values) != 0 {
			t.Fatal("old password remains claimable")
		}
	}
}
func TestPasswordResetFailsBeforeDispatchWhenInvalidationFails(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend}
	a.Incus = fake
	seedInitialPassword(t, a)
	_, err := a.Store.DB.Exec("CREATE TRIGGER fail_credential_update BEFORE UPDATE OF result_json ON task_items BEGIN SELECT RAISE(ABORT,'injected'); END")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ResetPassword("guest", "new-fixture-password"); err == nil {
		t.Fatal("credential invalidation failure ignored")
	}
	if fake.calls != 0 {
		t.Fatal("password changed before credentials were invalidated")
	}
}
func TestPasswordResetRejectsInvalidInputWithoutDiscardingCredential(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend}
	a.Incus = fake
	seedInitialPassword(t, a)
	if _, err := a.ResetPassword("guest", "bad\nrecord"); err == nil {
		t.Fatal("invalid password accepted")
	}
	values, err := a.ClaimInitialCredentials("credentials")
	if err != nil || len(values) != 1 || fake.calls != 0 {
		t.Fatal("rejected input changed credentials")
	}
}
func TestPasswordResetsSerializeCanonicalIdentityAndReleaseLocks(t *testing.T) {
	a, backend := testApp(t)
	if _, err := a.Store.DB.Exec("UPDATE instances SET name='alias' WHERE id='guest'"); err != nil {
		t.Fatal(err)
	}
	fake := &passwordBackend{resourceBackend: backend, entered: make(chan string, 2), release: make(chan struct{})}
	a.Incus = fake
	done := make(chan error, 2)
	go func() { _, err := a.ResetPassword("guest", "first-fixture-password"); done <- err }()
	select {
	case <-fake.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first request stalled")
	}
	go func() { _, err := a.ResetPassword("alias", "second-fixture-password"); done <- err }()
	select {
	case <-fake.entered:
		t.Error("alias bypassed instance lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(fake.release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if len(a.instanceLocks) != 0 {
		t.Fatal("unused instance locks retained")
	}
}
func TestPasswordOperationsOnDifferentInstancesCanOverlap(t *testing.T) {
	a, backend := testApp(t)
	_, err := a.Store.DB.Exec("INSERT INTO instances(id,name,incus_name,display_name,image,cpu_cores,memory_mib,disk_gib,stack_mode,desired_power,created_at) VALUES('other','other','p-other','other','image',0.5,128,1,'v4','running',0)")
	if err != nil {
		t.Fatal(err)
	}
	fake := &passwordBackend{resourceBackend: backend, entered: make(chan string, 2), release: make(chan struct{})}
	a.Incus = fake
	done := make(chan error, 2)
	for _, id := range []string{"guest", "other"} {
		go func(id string) { _, err := a.ResetPassword(id, "fixture-password"); done <- err }(id)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-fake.entered:
		case <-time.After(3 * time.Second):
			close(fake.release)
			t.Fatal("independent instances were serialized")
		}
	}
	close(fake.release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
func TestPowerWaitsForSameInstancePasswordOperation(t *testing.T) {
	a, backend := testApp(t)
	fake := &passwordBackend{resourceBackend: backend, entered: make(chan string, 1), release: make(chan struct{}), power: make(chan string, 1)}
	a.Incus = fake
	done := make(chan error, 2)
	go func() { _, err := a.ResetPassword("guest", "fixture-password"); done <- err }()
	select {
	case <-fake.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reset stalled")
	}
	go func() { done <- a.Power("guest", "stop", false) }()
	select {
	case <-fake.power:
		t.Error("power operation overlapped reset")
	case <-time.After(100 * time.Millisecond):
	}
	close(fake.release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
