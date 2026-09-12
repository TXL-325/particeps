package core

import (
	"database/sql"
	"errors"
	"fmt"

	"particeps/internal/incusx"
	"particeps/internal/store"
)

// Called under the canonical instance lock. Never store the password here.
// The pre-dispatch row survives process loss before an operation reference
// arrives; such an outcome requires manual reconciliation.
func (a *App) setInstancePassword(id, name, password string) error {
	if _, err := a.Store.DB.Exec(`INSERT INTO password_updates(instance_id,created_at) VALUES(?,?)`, id, store.Now()); err != nil {
		return fmt.Errorf("password operation could not be recorded: %w", err)
	}
	err := a.Incus.SetRootPassword(name, password)
	operation := incusx.ExecOperationState(err)
	if operation.Terminal {
		_, clearErr := a.Store.DB.Exec(`DELETE FROM password_updates WHERE instance_id=?`, id)
		if clearErr != nil {
			return errors.Join(err, fmt.Errorf("password completion could not be saved; reconciliation required: %w", clearErr))
		}
		return err
	}
	if operation.ID != "" {
		_, saveErr := a.Store.DB.Exec(`UPDATE password_updates SET operation=? WHERE instance_id=?`, operation.ID, id)
		err = errors.Join(err, saveErr)
	}
	return fmt.Errorf("password operation completion is unknown; wait for reconciliation before retrying: %w", err)
}

func (a *App) reconcilePasswordOperation(id string) error {
	var operation string
	err := a.Store.DB.QueryRow(`SELECT operation FROM password_updates WHERE instance_id=?`, id).Scan(&operation)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if operation == "" {
		return fmt.Errorf("an earlier password operation has no confirmed reference; manual reconciliation is required")
	}
	finished, err := a.Incus.ConfigOperationFinished(operation)
	if err != nil {
		return fmt.Errorf("password operation cannot be reconciled: %w", err)
	}
	if !finished {
		return fmt.Errorf("an earlier password operation is still running; please retry after it finishes")
	}
	_, err = a.Store.DB.Exec(`DELETE FROM password_updates WHERE instance_id=?`, id)
	return err
}
