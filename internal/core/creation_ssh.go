package core

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"particeps/internal/incusx"
	"particeps/internal/store"
)

// Called under the instance lock before creation publishes any forwards.
// Persist before dispatch: losing the process or an exec response does not
// prove the guest command stopped, even if its websocket has been closed.
func (a *App) provisionSSH(id, phase string, run func() error) error {
	message := fmt.Sprintf("SSH %s operation has no confirmed completion; manual reconciliation required", phase)
	result, err := a.Store.DB.Exec(`INSERT INTO instance_network(instance_id,status,uncertain,last_error,updated_at)
		VALUES(?,'needs-reconciliation',1,?,?) ON CONFLICT(instance_id) DO UPDATE SET
		status='needs-reconciliation',uncertain=1,last_error=excluded.last_error,updated_at=excluded.updated_at
		WHERE instance_network.uncertain=0 AND instance_network.deleting=0`, id, message, store.Now())
	if err != nil {
		return fmt.Errorf("SSH %s operation could not be recorded: %w", phase, err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return fmt.Errorf("SSH %s cannot start while an earlier instance operation needs reconciliation", phase)
	}
	err = run()
	operation := incusx.ExecOperationState(err)
	if !operation.Terminal {
		// Keep only the operation path. Transport errors can contain websocket
		// query secrets; neither those diagnostics nor command input belongs here.
		reference := "unavailable"
		if u, parseErr := url.Parse(operation.ID); parseErr == nil && strings.HasPrefix(u.Path, "/1.0/operations/") {
			reference = u.EscapedPath()
		}
		failure := fmt.Errorf("SSH %s operation %s has no confirmed completion; manual reconciliation required", phase, reference)
		_, saveErr := a.Store.DB.Exec(`UPDATE instance_network SET last_error=?,updated_at=? WHERE instance_id=?`, failure.Error(), store.Now(), id)
		return errors.Join(failure, saveErr)
	}
	message = ""
	if err != nil {
		message = fmt.Sprintf("SSH %s failed: %s", phase, err)
	}
	result, saveErr := a.Store.DB.Exec(`UPDATE instance_network SET uncertain=0,status='pending',last_error=?,updated_at=? WHERE instance_id=?`, message, store.Now(), id)
	if saveErr == nil {
		if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
			saveErr = fmt.Errorf("SSH operation record is missing")
		}
	}
	if saveErr != nil {
		return errors.Join(err, fmt.Errorf("SSH %s completion could not be saved; manual reconciliation required: %w", phase, saveErr))
	}
	return err
}
