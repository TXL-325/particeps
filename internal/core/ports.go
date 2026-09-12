package core

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func parsePortNumbers(value string) ([]int, error) {
	var ports []int
	for _, item := range strings.Split(value, ",") {
		bounds := strings.Split(strings.TrimSpace(item), "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("invalid port range %q", value)
		}
		start, err := strconv.Atoi(bounds[0])
		if err != nil || start < 1 || start > 65535 {
			return nil, fmt.Errorf("invalid port range %q", value)
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(bounds[1])
			if err != nil || end < start || end > 65535 {
				return nil, fmt.Errorf("invalid port range %q", value)
			}
		}
		if len(ports)+end-start+1 > 65535 {
			return nil, fmt.Errorf("port range is too large")
		}
		for number := start; number <= end; number++ {
			ports = append(ports, number)
		}
	}
	return ports, nil
}

// Inspect sockets without binding temporary listeners. Both protocols and both
// address families reserve a number, matching the shared TCP/UDP allocation rule.
func readHostPorts() (map[int]bool, error) {
	return readHostPortsAt("/proc/net")
}

func readHostPortsAt(root string) (map[int]bool, error) {
	used := map[int]bool{}
	for _, name := range []string{"tcp", "tcp6", "udp", "udp6"} {
		file, err := os.Open(root + "/" + name)
		if err != nil {
			if strings.HasSuffix(name, "6") && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read host %s sockets: %w", name, err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 4 || fields[0] == "sl" {
				continue
			}
			// Accepted/connected sockets still own their local port after a
			// listener closes. Include them (and TIME_WAIT) before DNAT or direct
			// conntrack cleanup can claim this number for a guest.
			parts := strings.Split(fields[1], ":")
			if len(parts) != 2 {
				_ = file.Close()
				return nil, fmt.Errorf("invalid host socket address")
			}
			port, err := strconv.ParseUint(parts[1], 16, 16)
			if err != nil {
				_ = file.Close()
				return nil, fmt.Errorf("invalid host socket port")
			}
			if port > 0 {
				used[int(port)] = true
			}
		}
		err = scanner.Err()
		_ = file.Close()
		if err != nil {
			return nil, err
		}
	}
	return used, nil
}

func (a *App) occupiedPortsLocked(listen string) (map[int]bool, error) {
	pending, err := a.addressWritePending(listen)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, fmt.Errorf("ingress address has an unfinished write; reconciliation required")
	}
	probe := a.hostPorts
	if probe == nil {
		probe = readHostPorts
	}
	used, err := probe()
	if err != nil {
		return nil, err
	}
	forwards, err := a.Incus.ListForwards(a.Cfg.Network)
	if err != nil {
		return nil, fmt.Errorf("inspect existing Incus forwards: %w", err)
	}
	for _, forward := range forwards {
		if forward.ListenAddress == listen {
			owner, err := a.forwardOwner()
			if err != nil {
				return nil, err
			}
			if err := requireForwardOwner(forward, owner); err != nil {
				return nil, err
			}
		}
		if forward.ListenAddress == listen && forward.Config["target_address"] != "" {
			return nil, fmt.Errorf("ingress address already has a whole-address forward")
		}
		for _, p := range forward.Ports {
			numbers, err := parsePortNumbers(p.ListenPort)
			if err != nil {
				return nil, err
			}
			for _, number := range numbers {
				used[number] = true
			}
		}
	}
	return used, nil
}

func (a *App) checkHostPortConflicts(ports []Port) error {
	probe := a.hostPorts
	if probe == nil {
		probe = readHostPorts
	}
	used, err := probe()
	if err != nil {
		return err
	}
	for _, p := range ports {
		if used[p.Number] {
			return fmt.Errorf("host service now occupies port %d; reservation kept pending", p.Number)
		}
	}
	return nil
}

// AddPort reserves both protocols in one transaction before contacting Incus.
// If application fails the reservation and pending state survive a restart.
func (a *App) AddPort(id string, number int) error {
	a.allocationMu.Lock()
	defer a.allocationMu.Unlock()
	n, err := a.mutableNetwork(id)
	if err != nil {
		return err
	}
	if n.Listen == "" {
		return fmt.Errorf("instance has no IPv4 ingress address")
	}
	if n.Status == "pending" || n.Uncertain {
		return fmt.Errorf("resolve the pending port configuration before allocating another port")
	}
	used, err := a.occupiedPortsLocked(n.Listen)
	if err != nil {
		return err
	}
	if number == 0 {
		numbers, err := a.Store.NextPortsExcluding(1, a.Cfg.PortPoolStart, a.Cfg.PortPoolEnd, used)
		if err != nil {
			return err
		}
		number = numbers[0]
	} else {
		if number < a.Cfg.PortPoolStart || number > a.Cfg.PortPoolEnd || number < 1 || number > 65535 {
			return fmt.Errorf("port must be inside the configured pool")
		}
		var reserved bool
		if err := a.Store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ports WHERE number=?)`, number).Scan(&reserved); err != nil {
			return err
		}
		if reserved || used[number] {
			return fmt.Errorf("port %d is already occupied", number)
		}
	}
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, proto := range []string{"tcp", "udp"} {
		if _, err := tx.Exec(`INSERT INTO ports(instance_id,number,proto,listen_ip,target) VALUES(?,?,?,?,?)`, n.ID, number, proto, n.Listen, number); err != nil {
			return err
		}
	}
	if err := recordNetworkPending(tx, n.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return a.syncPortsLocked(n, true)
}

func (a *App) EditPort(id string, number int, proto string, target int) error {
	if (proto != "tcp" && proto != "udp") || number < 1 || number > 65535 || target < 1 || target > 65535 {
		return fmt.Errorf("invalid port number, protocol, or target")
	}
	a.allocationMu.Lock()
	defer a.allocationMu.Unlock()
	n, err := a.mutableNetwork(id)
	if err != nil {
		return err
	}
	if n.Status == "pending" || n.Uncertain {
		return fmt.Errorf("resolve the pending port configuration before editing a target")
	}
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE ports SET target=? WHERE instance_id=? AND number=? AND proto=?`, target, n.ID, number, proto)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return fmt.Errorf("port does not belong to this instance")
	}
	if err := recordNetworkPending(tx, n.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return a.syncPortsLocked(n, true)
}
