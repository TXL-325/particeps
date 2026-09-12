package core

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"particeps/internal/auth"
)

func (a *App) authenticationMaintenance() {
	defer close(a.authDone)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-a.collectStop:
			return
		case now := <-ticker.C:
			if err := a.Store.PruneAuthentication(now); err != nil {
				log.Printf("authentication maintenance failed: %v", err)
			}
		}
	}
}

type instanceLock struct {
	mu    sync.Mutex
	users int
}

// Obtain the canonical instance lock before allocationMu/resourceMu. Entries
// are removed after the final waiter, so deleted instances do not leak locks.
func (a *App) lockInstance(id string) func() {
	a.instanceLocksMu.Lock()
	if a.instanceLocks == nil {
		a.instanceLocks = make(map[string]*instanceLock)
	}
	entry := a.instanceLocks[id]
	if entry == nil {
		entry = &instanceLock{}
		a.instanceLocks[id] = entry
	}
	entry.users++
	a.instanceLocksMu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		a.instanceLocksMu.Lock()
		entry.users--
		if entry.users == 0 {
			delete(a.instanceLocks, id)
		}
		a.instanceLocksMu.Unlock()
	}
}

func (a *App) lockExistingInstance(id string) (string, func(), error) {
	n, err := a.networkRecord(id)
	if err != nil {
		return "", nil, err
	}
	creating, err := a.instanceCreating(n.ID)
	if err != nil {
		return "", nil, err
	}
	if creating {
		return "", nil, fmt.Errorf("instance creation is still in progress")
	}
	unlock := a.lockInstance(n.ID)
	// The original object may have been deleted while this caller waited. Do
	// not re-resolve its ID as another instance's display name after the wait.
	var canonical string
	if err := a.Store.DB.QueryRow(`SELECT id FROM instances WHERE id=?`, n.ID).Scan(&canonical); err != nil {
		unlock()
		return "", nil, err
	}
	return canonical, unlock, nil
}

func (a *App) ResetPassword(id, password string) (string, error) {
	id, unlock, err := a.lockExistingInstance(id)
	if err != nil {
		return "", err
	}
	defer unlock()
	n, err := a.mutableNetwork(id)
	if err != nil {
		return "", err
	}
	if n.Uncertain {
		return "", fmt.Errorf("an earlier power or forward operation needs reconciliation before changing the password")
	}
	if err := a.reconcilePasswordOperation(id); err != nil {
		return "", err
	}
	state, err := a.Incus.GetState(n.IncusName)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(state.Status, "Running") {
		return "", fmt.Errorf("password reset requires a running instance")
	}
	if strings.ContainsAny(password, "\r\n\x00") {
		return "", fmt.Errorf("password must be a single line")
	}
	if password == "" {
		password = auth.NewTokenPlain()[:16]
	}
	// Invalidate before dispatch, including an uncertain reset. A failed database
	// write must prevent dispatch; a possibly changed password cannot be handed
	// out later through the original initialization task.
	if _, err := a.Store.DB.Exec(`UPDATE task_items SET result_json='' WHERE instance_id=?`, id); err != nil {
		return "", fmt.Errorf("initial credentials could not be invalidated; password was not changed: %w", err)
	}
	if err := a.setInstancePassword(id, n.IncusName, password); err != nil {
		return "", err
	}
	return password, nil
}
