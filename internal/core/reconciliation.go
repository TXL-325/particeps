package core

import (
	"encoding/json"
	"fmt"

	"particeps/internal/incusx"
	"particeps/internal/store"
)

type resourceValues struct {
	CPU       float64
	Pin       string
	Memory    int
	Disk      int
	Bandwidth int
}

type resourceUpdate struct {
	Operation                   incusx.ConfigOperation
	IncusName                   string
	Before, After               resourceValues
	BeforeConfig, AfterConfig   map[string]string
	BeforeDevices, AfterDevices map[string]map[string]string
}

func (a *App) recordResourceOperation(id string, operation incusx.ConfigOperation) error {
	data, err := json.Marshal(operation)
	if err != nil {
		return err
	}
	result, err := a.Store.DB.Exec(`UPDATE resource_updates SET request_json=json_set(request_json,'$.Operation',json(?)) WHERE instance_id=?`, string(data), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return fmt.Errorf("pending resource update could not be recorded")
	}
	return nil
}

func resourceValuesOf(in Instance) resourceValues {
	return resourceValues{in.CPUCores, in.CPUPin, in.MemoryMiB, in.DiskGiB, in.BandwidthMbps}
}

func decodeConfig(value any) (map[string]string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var config map[string]string
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid Incus config: %w", err)
	}
	return config, nil
}

func (a *App) recordResourceUpdate(before, after Instance, current map[string]any, config map[string]string, devices map[string]map[string]string) error {
	oldConfig, err := decodeConfig(current["config"])
	if err != nil {
		return err
	}
	oldDevices, err := decodeDevices(current["devices"])
	if err != nil {
		return err
	}
	update := resourceUpdate{IncusName: before.IncusName, Before: resourceValuesOf(before), After: resourceValuesOf(after),
		BeforeConfig: map[string]string{}, AfterConfig: config, BeforeDevices: map[string]map[string]string{}, AfterDevices: devices}
	for key := range config {
		update.BeforeConfig[key] = oldConfig[key]
	}
	for name := range devices {
		update.BeforeDevices[name] = oldDevices[name]
	}
	data, err := json.Marshal(update)
	if err != nil {
		return err
	}
	_, err = a.Store.DB.Exec(`INSERT INTO resource_updates(instance_id,request_json,created_at) VALUES(?,?,?)`, before.ID, string(data), store.Now())
	return err
}

func resourceConfigMatches(actual map[string]any, expectedConfig map[string]string, expectedDevices map[string]map[string]string) (bool, error) {
	config, err := decodeConfig(actual["config"])
	if err != nil {
		return false, err
	}
	devices, err := decodeDevices(actual["devices"])
	if err != nil {
		return false, err
	}
	for key, value := range expectedConfig {
		if config[key] != value {
			return false, nil
		}
	}
	for name, device := range expectedDevices {
		for key, value := range device {
			if devices[name][key] != value {
				return false, nil
			}
		}
	}
	return true, nil
}

func (a *App) finishResourceUpdate(id string, values resourceValues) error {
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE instances SET cpu_cores=?,cpu_pin=?,memory_mib=?,disk_gib=?,bandwidth_mbps=? WHERE id=?`,
		values.CPU, values.Pin, values.Memory, values.Disk, values.Bandwidth, id)
	if err != nil {
		return fmt.Errorf("resource record could not be saved; reconciliation required: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return fmt.Errorf("resource record is missing; reconciliation required")
	}
	if _, err := tx.Exec(`DELETE FROM resource_updates WHERE instance_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Call with resourceMu held before resource/cap changes or new reservations.
// An intent survives failures and restarts; unresolved backend state blocks
// changes instead of using a stale database quota for capacity decisions.
func (a *App) reconcileResourcesLocked() error {
	rows, err := a.Store.DB.Query(`SELECT instance_id,request_json FROM resource_updates`)
	if err != nil {
		return err
	}
	type pending struct{ id, raw string }
	items := []pending{}
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.raw); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		var update resourceUpdate
		if err := json.Unmarshal([]byte(item.raw), &update); err != nil {
			return fmt.Errorf("instance %s has an invalid pending resource update", item.id)
		}
		if !update.Operation.Terminal {
			if update.Operation.ID == "" {
				return fmt.Errorf("instance %s resource operation outcome is unknown; reconciliation requires operator review", item.id)
			}
			finished, err := a.Incus.ConfigOperationFinished(update.Operation.ID)
			if err != nil {
				return fmt.Errorf("instance %s resource operation cannot be confirmed: %w", item.id, err)
			}
			if !finished {
				return fmt.Errorf("instance %s resource operation is still running", item.id)
			}
			update.Operation.Terminal = true
			if err := a.recordResourceOperation(item.id, update.Operation); err != nil {
				return err
			}
		}
		actual, err := a.Incus.GetConfig(update.IncusName)
		if err != nil {
			return fmt.Errorf("instance %s needs resource reconciliation: %w", item.id, err)
		}
		after, err := resourceConfigMatches(actual, update.AfterConfig, update.AfterDevices)
		if err != nil {
			return err
		}
		values := update.After
		if !after {
			before, err := resourceConfigMatches(actual, update.BeforeConfig, update.BeforeDevices)
			if err != nil {
				return err
			}
			if !before {
				return fmt.Errorf("instance %s has partially applied resources; reconciliation required", item.id)
			}
			values = update.Before
		}
		if err := a.finishResourceUpdate(item.id, values); err != nil {
			return err
		}
	}
	return nil
}
