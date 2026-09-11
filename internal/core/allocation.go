package core

import (
	"fmt"

	"particeps/internal/store"
)

func (a *App) reserveInstance(id, name, incusName string, req CreateReq, pwLogin bool, nat4, v6addr, v6mode string) error {
	// Serialize the capacity check with cap updates and serialize allocation with
	// other workers. A single SQLite transaction owns all TCP/UDP rows together.
	a.resourceMu.Lock()
	defer a.resourceMu.Unlock()
	if err := a.reconcileResourcesLocked(); err != nil {
		return err
	}
	a.allocationMu.Lock()
	defer a.allocationMu.Unlock()
	capCores, status := a.CPUCapStatus()
	if !status.Applied || req.CPUCores > capCores {
		return fmt.Errorf("aggregate CPU cap is unavailable or below requested quota")
	}
	ports, err := a.Store.NextPorts(a.Cfg.PortsPerGuest, a.Cfg.PortPoolStart, a.Cfg.PortPoolEnd)
	if err != nil {
		return err
	}
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO instances(id,name,incus_name,display_name,image,cpu_cores,cpu_pin,memory_mib,disk_gib,bandwidth_mbps,stack_mode,desired_power,ssh_pubkey,password_login,nat_ipv4,ipv6,ipv6_mode,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, name, incusName, name, req.Image, req.CPUCores, req.CPUPin, req.MemoryMiB, req.DiskGiB, req.BandwidthMbps, req.StackMode, "running", req.SSHPubKey, b2i(pwLogin), nat4, v6addr, v6mode, store.Now())
	if err != nil {
		return err
	}
	listenIP := nat4
	if req.StackMode == "v6" {
		listenIP = v6addr
	}
	if listenIP == "" {
		return fmt.Errorf("no selected ingress address")
	}
	for index, number := range ports {
		for _, proto := range []string{"tcp", "udp"} {
			target := number
			if index == 0 && proto == "tcp" {
				target = 22
			}
			if _, err := tx.Exec(`INSERT INTO ports(instance_id,number,proto,listen_ip,target) VALUES(?,?,?,?,?)`, id, number, proto, listenIP, target); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
