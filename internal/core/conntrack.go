package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"particeps/internal/incusx"
)

func conntrackCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "conntrack", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	return string(out), err
}

func clearNATConnections(ports []Port) error {
	return clearNATConnectionsWith(ports, conntrackCommand)
}

func clearNATConnectionsWith(ports []Port, run func(...string) (string, error)) error {
	for _, p := range ports {
		ip := net.ParseIP(p.ListenIP)
		if ip == nil || ip.To4() == nil || (p.Proto != "tcp" && p.Proto != "udp") || p.Number < 1 || p.Number > 65535 {
			return fmt.Errorf("invalid NAT connection cleanup key")
		}
		filter := []string{"-f", "ipv4", "-p", p.Proto, "--orig-dst", ip.String(), "--orig-port-dst", strconv.Itoa(p.Number)}
		if p.directBinding {
			if p.replyAddress != p.ListenIP || p.Target != p.Number {
				return fmt.Errorf("invalid direct connection cleanup key")
			}
		} else {
			filter = append(filter, "--dst-nat")
		}
		if p.replyAddress != "" {
			reply := net.ParseIP(p.replyAddress)
			if reply == nil || reply.To4() == nil || p.Target < 1 || p.Target > 65535 {
				return fmt.Errorf("invalid previous NAT target")
			}
			filter = append(filter, "--reply-src", reply.String(), "--reply-port-src", strconv.Itoa(p.Target))
		}
		_, deleteErr := run(append([]string{"-D"}, filter...)...)
		if deleteErr != nil {
			// conntrack returns 1 for zero deleted entries. Only accept that code
			// after an independent, successful query proves no matching NAT remains.
			var exited interface{ ExitCode() int }
			if !errors.As(deleteErr, &exited) || exited.ExitCode() != 1 {
				return fmt.Errorf("clear NAT connections for %s:%d/%s: %w", p.ListenIP, p.Number, p.Proto, deleteErr)
			}
		}
		out, err := run(append([]string{"-L"}, filter...)...)
		if err != nil {
			return fmt.Errorf("verify NAT connection cleanup for %s:%d/%s: %w", p.ListenIP, p.Number, p.Proto, err)
		}
		if strings.TrimSpace(out) != "" {
			return fmt.Errorf("NAT connections remain for %s:%d/%s", p.ListenIP, p.Number, p.Proto)
		}
	}
	return nil
}

func (a *App) queueConntrackCleanup(id string, ports []Port) error {
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range ports {
		replyPort := p.Target
		if p.replyAddress == "" {
			replyPort = 0
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO conntrack_cleanup(instance_id,listen_address,number,proto,reply_address,reply_port,direct_binding) VALUES(?,?,?,?,?,?,?)`, id, p.ListenIP, p.Number, p.Proto, p.replyAddress, replyPort, p.directBinding); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func changedForwardPorts(listen, tag string, before, after []incusx.ForwardPort) ([]Port, error) {
	var changed []Port
	for _, old := range before {
		if old.Description != tag {
			continue
		}
		unchanged := false
		for _, next := range after {
			if old.Protocol == next.Protocol && old.ListenPort == next.ListenPort && old.TargetPort == next.TargetPort && old.TargetAddress == next.TargetAddress {
				unchanged = true
				break
			}
		}
		if unchanged {
			continue
		}
		numbers, err := parsePortNumbers(old.ListenPort)
		if err != nil {
			return nil, err
		}
		// Agent rules use one target port per entry. Keep the old reply tuple so
		// a fresh connection to the new target cannot be mistaken for stale NAT.
		target, err := strconv.Atoi(old.TargetPort)
		if err != nil || target < 1 || target > 65535 {
			return nil, fmt.Errorf("cannot reconcile previous target port")
		}
		for _, number := range numbers {
			changed = append(changed, Port{Number: number, Proto: old.Protocol, ListenIP: listen, replyAddress: old.TargetAddress, Target: target})
		}
	}
	return changed, nil
}

func (a *App) conntrackCleanupPorts(id, listen string) ([]Port, error) {
	rows, err := a.Store.DB.Query(`SELECT listen_address,number,proto,reply_address,reply_port,direct_binding FROM conntrack_cleanup WHERE instance_id=? AND (?='' OR listen_address=?) ORDER BY listen_address,number,proto,reply_address,reply_port`, id, listen, listen)
	if err != nil {
		return nil, err
	}
	var ports []Port
	for rows.Next() {
		var p Port
		if err := rows.Scan(&p.ListenIP, &p.Number, &p.Proto, &p.replyAddress, &p.Target, &p.directBinding); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ports = append(ports, p)
	}
	err = rows.Err()
	_ = rows.Close()
	return ports, err
}

func (a *App) flushConntrackCleanup(id string) error {
	return a.flushConntrackCleanupAt(id, "")
}

func (a *App) flushConntrackCleanupAt(id, listen string) error {
	return a.flushConntrackCleanupMode(id, listen, true)
}

func (a *App) flushConntrackCleanupMode(id, listen string, includeDirect bool) error {
	ports, err := a.conntrackCleanupPorts(id, listen)
	if err != nil {
		return err
	}
	if !includeDirect {
		filtered := ports[:0]
		for _, p := range ports {
			if !p.directBinding {
				filtered = append(filtered, p)
			}
		}
		ports = filtered
	}
	if len(ports) == 0 {
		return nil
	}
	clear := a.clearNAT
	if clear == nil {
		clear = clearNATConnections
	}
	if err := clear(ports); err != nil {
		return err
	}
	_, err = a.Store.DB.Exec(`DELETE FROM conntrack_cleanup WHERE instance_id=? AND (?='' OR listen_address=?) AND (? OR direct_binding=0)`, id, listen, listen, includeDirect)
	return err
}
