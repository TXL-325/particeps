package core

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"particeps/internal/incusx"
	"particeps/internal/store"
)

type networkRecord struct {
	ID, IncusName, Listen, Target, Status string
	Deleting                              bool
	Uncertain                             bool
}

func (a *App) networkRecord(id string) (networkRecord, error) {
	var n networkRecord
	err := a.Store.DB.QueryRow(`SELECT i.id,i.incus_name,i.nat_ipv4,COALESCE(n.target_ipv4,''),
		COALESCE(n.status,'unconfigured'),COALESCE(n.deleting,0),COALESCE(n.uncertain,0)
		FROM instances i LEFT JOIN instance_network n ON n.instance_id=i.id
		WHERE i.id=? OR i.name=? ORDER BY i.id=? DESC LIMIT 1`, id, id, id).
		Scan(&n.ID, &n.IncusName, &n.Listen, &n.Target, &n.Status, &n.Deleting, &n.Uncertain)
	return n, err
}

func (a *App) instanceCreating(id string) (bool, error) {
	var active bool
	err := a.Store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_items WHERE instance_id=? AND status IN ('pending','running'))`, id).Scan(&active)
	return active, err
}

func (a *App) mutableNetwork(id string) (networkRecord, error) {
	n, err := a.networkRecord(id)
	if err != nil {
		return n, err
	}
	if n.Deleting {
		return n, fmt.Errorf("instance deletion is pending; retry deletion to finish cleanup")
	}
	creating, err := a.instanceCreating(n.ID)
	if err != nil {
		return n, err
	}
	if creating {
		return n, fmt.Errorf("instance creation is still in progress")
	}
	return n, nil
}

func (a *App) markNetwork(id, status, message string) error {
	_, err := a.Store.DB.Exec(`INSERT INTO instance_network(instance_id,status,last_error,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(instance_id) DO UPDATE SET status=excluded.status,last_error=excluded.last_error,updated_at=excluded.updated_at`,
		id, status, message, store.Now())
	return err
}

func (a *App) networkFailure(id, status string, cause error) error {
	if incusx.IsForwardUncertain(cause) {
		_, err := a.Store.DB.Exec(`UPDATE instance_network SET uncertain=1,status='needs-reconciliation',last_error=?,updated_at=? WHERE instance_id=?`, cause.Error(), store.Now(), id)
		return errors.Join(cause, err)
	}
	return errors.Join(cause, a.markNetwork(id, status, cause.Error()))
}

// Called only with allocationMu held. The persistent identity also separates
// independent Agent databases sharing an Incus network.
func (a *App) forwardOwner() (string, error) {
	if _, err := a.Store.DB.Exec(`INSERT OR IGNORE INTO settings(k,v) VALUES('forward_owner',?)`, store.NewID()); err != nil {
		return "", err
	}
	var owner string
	if err := a.Store.DB.QueryRow(`SELECT v FROM settings WHERE k='forward_owner'`).Scan(&owner); err != nil {
		return "", err
	}
	if owner == "" {
		return "", fmt.Errorf("forward ownership identity is empty")
	}
	return owner, nil
}

func forwardTag(owner, id string) string { return "particeps/" + owner + "/" + id }

func requireForwardOwner(f incusx.Forward, owner string) error {
	if f.Config["user.particeps.owner"] != owner {
		return fmt.Errorf("ingress address %s belongs to an external forward; select a dedicated address", f.ListenAddress)
	}
	for _, p := range f.Ports {
		if p.TargetAddress == f.ListenAddress {
			return fmt.Errorf("forward target must not be the ingress address")
		}
		if !strings.HasPrefix(p.Description, "particeps/"+owner+"/") {
			return fmt.Errorf("ingress address %s contains external rules; refusing to replace the shared object", f.ListenAddress)
		}
	}
	return nil
}

// Incus 6.0.4 returns ETags but does not enforce If-Match on forward PUT.
// The supported boundary is therefore one Agent database owning the complete
// forward object. A durable address-level record serializes all its writers,
// including other App processes, and survives a crash during the HTTP request.
func (a *App) beginForwardWrite(listen, id string) (string, error) {
	token := store.NewID()
	_, err := a.Store.DB.Exec(`INSERT INTO forward_writes(network,listen_address,token,instance_id,phase,created_at) VALUES(?,?,?,?,'reading',?)`,
		a.Cfg.Network, listen, token, id, store.Now())
	if err != nil {
		return "", fmt.Errorf("address %s has an unfinished write or cannot be locked; reconciliation required: %w", listen, err)
	}
	return token, nil
}

func (a *App) endForwardWrite(token string) error {
	_, err := a.Store.DB.Exec(`DELETE FROM forward_writes WHERE token=?`, token)
	return err
}

func (a *App) addressWritePending(listen string) (bool, error) {
	var pending bool
	err := a.Store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM forward_writes WHERE network=? AND listen_address=?)`, a.Cfg.Network, listen).Scan(&pending)
	return pending, err
}

func forwardPorts(tag, target string, ports []Port) []incusx.ForwardPort {
	result := make([]incusx.ForwardPort, 0, len(ports))
	for _, p := range ports {
		if !p.Enabled {
			continue
		}
		result = append(result, incusx.ForwardPort{Description: tag, Protocol: p.Proto,
			ListenPort: strconv.Itoa(p.Number), TargetPort: strconv.Itoa(p.Target), TargetAddress: target})
	}
	return result
}

func taggedPorts(ports []incusx.ForwardPort, tag string) []incusx.ForwardPort {
	result := []incusx.ForwardPort{}
	for _, p := range ports {
		if p.Description == tag {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ListenPort == result[j].ListenPort {
			return result[i].Protocol < result[j].Protocol
		}
		return result[i].ListenPort < result[j].ListenPort
	})
	return result
}

func mergeForward(current incusx.Forward, tag string, desired []incusx.ForwardPort) (incusx.Forward, error) {
	if current.Config["target_address"] != "" {
		return current, fmt.Errorf("ingress address %s already has a whole-address forward; manual reconciliation required", current.ListenAddress)
	}
	numbers := map[int]bool{}
	for _, p := range desired {
		number, err := strconv.Atoi(p.ListenPort)
		if err != nil {
			return current, err
		}
		numbers[number] = true
	}
	retained := make([]incusx.ForwardPort, 0, len(current.Ports)+len(desired))
	for _, p := range current.Ports {
		if p.Description == tag {
			continue
		}
		occupied, err := parsePortNumbers(p.ListenPort)
		if err != nil {
			return current, fmt.Errorf("cannot validate existing forward: %w", err)
		}
		for _, number := range occupied {
			if numbers[number] {
				return current, fmt.Errorf("port %d on %s is owned by another forward", number, current.ListenAddress)
			}
		}
		retained = append(retained, p)
	}
	current.Ports = append(retained, desired...)
	return current, nil
}

// One Incus object belongs to an ingress address, not an instance. Preserve the
// other instances in that owned object; never merge into an external object.
func (a *App) updateInstanceForward(n networkRecord, listen, target string, ports []Port, remove bool) (resultErr error) {
	ip := net.ParseIP(listen)
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() {
		return fmt.Errorf("a usable IPv4 ingress address is required")
	}
	if !remove && listen == target {
		return fmt.Errorf("ingress and guest IPv4 addresses must differ")
	}
	owner, err := a.forwardOwner()
	if err != nil {
		return err
	}
	token, err := a.beginForwardWrite(listen, n.ID)
	if err != nil {
		return err
	}
	uncertain := false
	defer func() {
		if !uncertain {
			resultErr = errors.Join(resultErr, a.endForwardWrite(token))
		}
	}()
	tag := forwardTag(owner, n.ID)
	desired := forwardPorts(tag, target, ports)
	if remove {
		desired = []incusx.ForwardPort{}
		// Host-directed cached flows cannot reach a guest and do not need to be
		// drained while stopping. They will be handled on the next activation.
		if _, err := a.Store.DB.Exec(`DELETE FROM conntrack_cleanup WHERE instance_id=? AND listen_address=? AND direct_binding=1`, n.ID, listen); err != nil {
			return err
		}
		active := make([]Port, 0, len(ports))
		for _, p := range ports {
			if p.Enabled {
				active = append(active, p)
			}
		}
		if err := a.queueConntrackCleanup(n.ID, active); err != nil {
			return err
		}
	}
	write := func(next incusx.Forward, etag string, create bool) (incusx.Forward, string, error) {
		if _, err := a.Store.DB.Exec("UPDATE forward_writes SET phase='writing' WHERE token=?", token); err != nil {
			return incusx.Forward{}, "", err
		}
		uncertain = true
		var err error
		if create {
			err = a.Incus.CreateForward(a.Cfg.Network, next)
		} else {
			err = a.Incus.UpdateForward(a.Cfg.Network, next, etag)
		}
		if !incusx.IsForwardUncertain(err) {
			uncertain = false
		}
		if err != nil {
			return incusx.Forward{}, "", err
		}
		actual, revision, err := a.Incus.GetForward(a.Cfg.Network, listen)
		if err != nil {
			return actual, revision, fmt.Errorf("forward readback failed: %w", err)
		}
		if err := requireForwardOwner(actual, owner); err != nil {
			return actual, revision, err
		}
		if !slices.Equal(actual.Ports, next.Ports) || !maps.Equal(actual.Config, next.Config) || actual.Description != next.Description {
			return actual, revision, fmt.Errorf("forward readback differs from the requested rules")
		}
		return actual, revision, nil
	}
	conflict := func(err error) bool {
		return incusx.IsStatus(err, http.StatusPreconditionFailed) || incusx.IsStatus(err, http.StatusConflict)
	}
	for attempt := 0; attempt < 4; attempt++ {
		current, etag, err := a.Incus.GetForward(a.Cfg.Network, listen)
		missing := incusx.IsStatus(err, http.StatusNotFound)
		if err != nil && !missing {
			return err
		}
		if missing {
			if len(desired) == 0 {
				return a.flushConntrackCleanupAt(n.ID, listen)
			}
			current = incusx.Forward{ListenAddress: listen, Description: "Particeps port mappings",
				Config: map[string]string{"user.particeps.owner": owner}, Ports: []incusx.ForwardPort{}}
		}
		if err := requireForwardOwner(current, owner); err != nil {
			return err
		}
		if _, err := mergeForward(current, tag, desired); err != nil {
			return err
		}
		if len(desired) > 0 {
			if err := a.checkHostPortConflicts(ports); err != nil {
				return err
			}
		}
		changed, err := changedForwardPorts(listen, tag, current.Ports, desired)
		if err != nil {
			return err
		}
		if remove {
			// A failed disable may leave an actual rule whose ledger is already
			// disabled. Drain it too; the enabled rows alone are insufficient.
			for i := range changed {
				changed[i].replyAddress = ""
			}
		}
		if err := a.queueConntrackCleanup(n.ID, changed); err != nil {
			return err
		}
		queued, err := a.conntrackCleanupPorts(n.ID, listen)
		if err != nil {
			return err
		}
		broadCleanup := false
		for _, p := range queued {
			broadCleanup = broadCleanup || p.replyAddress == ""
		}
		if broadCleanup {
			// A previous stop/address-loss cleanup may have failed. Complete that
			// cleanup while this instance's rules are absent, before restoring any
			// mapping that could create fresh matching DNAT connections.
			if !missing && len(taggedPorts(current.Ports, tag)) > 0 {
				inactive, err := mergeForward(current, tag, nil)
				if err != nil {
					return err
				}
				current, etag, err = write(inactive, etag, false)
				if conflict(err) {
					continue
				}
				if err != nil {
					return err
				}
			}
			if err := a.flushConntrackCleanupMode(n.ID, listen, false); err != nil {
				return err
			}
		}
		if !remove {
			// Traffic arriving during downtime can cache a non-NAT reply tuple
			// pointing to the host itself. Clear that exact tuple after installing
			// DNAT; fresh guest-directed bindings have a different reply address.
			direct := make([]Port, 0, len(ports))
			for _, p := range ports {
				if !p.Enabled {
					continue
				}
				direct = append(direct, Port{Number: p.Number, Proto: p.Proto, ListenIP: listen, Target: p.Number, replyAddress: listen, directBinding: true})
			}
			if err := a.queueConntrackCleanup(n.ID, direct); err != nil {
				return err
			}
		}
		final, err := mergeForward(current, tag, desired)
		if err != nil {
			return err
		}
		if !missing && reflect.DeepEqual(taggedPorts(current.Ports, tag), taggedPorts(desired, tag)) {
			return a.flushConntrackCleanupAt(n.ID, listen)
		}
		_, _, err = write(final, etag, missing)
		if conflict(err) {
			continue
		}
		if err != nil {
			return err
		}
		// For an edit, only old reply tuples are removed. New connections to
		// the new target remain valid even under continuous UDP traffic.
		return a.flushConntrackCleanupAt(n.ID, listen)
	}
	return fmt.Errorf("forward changed concurrently; retry after inspecting the current rules")
}

func (a *App) managedGuestIPv4(st *incusx.InstanceState) string {
	if st == nil {
		return ""
	}
	_, subnet, err := net.ParseCIDR(a.Cfg.PrivateIPv4CIDR)
	if err != nil {
		return ""
	}
	for _, address := range st.Network["eth0"].Addresses {
		ip := net.ParseIP(address.Address)
		if address.Family == "inet" && ip != nil && ip.To4() != nil && subnet.Contains(ip) {
			return ip.String()
		}
	}
	return ""
}

func (a *App) syncPortsLocked(n networkRecord, waitForAddress bool) error {
	if n.Uncertain {
		return fmt.Errorf("an earlier instance operation may still be active; manual reconciliation required")
	}
	if n.Deleting {
		return fmt.Errorf("instance deletion is pending")
	}
	ports, err := a.instancePorts(n.ID)
	if err != nil || len(ports) == 0 {
		return err
	}
	if err := a.markNetwork(n.ID, "pending", ""); err != nil {
		return err
	}
	fail := func(err error) error { return a.networkFailure(n.ID, "pending", err) }
	hasEnabled := false
	for _, p := range ports {
		hasEnabled = hasEnabled || p.Enabled
	}
	var target string
	for attempt := 0; attempt < 121; attempt++ {
		state, err := a.Incus.GetState(n.IncusName)
		if err != nil {
			return fail(err)
		}
		target = a.managedGuestIPv4(state)
		if strings.EqualFold(state.Status, "Stopped") {
			// Dynamic DHCP addresses cannot be trusted after a guest stops. Keep
			// its number/target ledger, but remove the actual DNAT before reuse.
			if err := a.cleanupForwardsLocked(n); err != nil {
				return fail(err)
			}
			return a.markNetwork(n.ID, "inactive", "")
		}
		// Disabling the final mapping must remove old rules even while DHCP
		// has no address. Reserved ports do not need a reachable target.
		if !hasEnabled {
			break
		}
		if target == "" && attempt == 0 {
			if err := a.cleanupForwardsLocked(n); err != nil {
				return fail(err)
			}
			if err := a.markNetwork(n.ID, "waiting-address", "waiting for a usable eth0 IPv4 address"); err != nil {
				return err
			}
		}
		if target != "" || !waitForAddress || attempt == 120 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if target == "" && hasEnabled {
		return fmt.Errorf("no IPv4 address on the managed eth0 network yet; forwarding is disabled while waiting")
	}
	grouped := map[string][]Port{}
	for _, p := range ports {
		grouped[p.ListenIP] = append(grouped[p.ListenIP], p)
	}
	for listen, group := range grouped {
		if err := a.updateInstanceForward(n, listen, target, group, false); err != nil {
			return fail(err)
		}
	}
	if err := a.flushConntrackCleanup(n.ID); err != nil {
		return fail(err)
	}
	_, err = a.Store.DB.Exec(`UPDATE instance_network SET target_ipv4=?,status='ready',last_error='',updated_at=? WHERE instance_id=?`, target, store.Now(), n.ID)
	return err
}

func (a *App) SyncPorts(id string) error {
	a.allocationMu.Lock()
	defer a.allocationMu.Unlock()
	n, err := a.mutableNetwork(id)
	if err != nil {
		return err
	}
	return a.syncPortsLocked(n, true)
}

func (a *App) applyForwards(id, incusName, _ string) error {
	// First-boot DHCP can outlast password provisioning. Wait for the observed
	// address outside allocationMu so one slow guest cannot block other guests.
	deadline := time.Now().Add(45 * time.Second)
	waiting := false
	for {
		state, err := a.Incus.GetState(incusName)
		if err != nil {
			return err
		}
		if a.managedGuestIPv4(state) != "" {
			break
		}
		if !waiting {
			if err := a.markNetwork(id, "waiting-address", "waiting for the first IPv4 lease"); err != nil {
				return err
			}
			waiting = true
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("initial IPv4 lease was not ready within 45 seconds")
		}
		time.Sleep(250 * time.Millisecond)
	}
	a.allocationMu.Lock()
	defer a.allocationMu.Unlock()
	n, err := a.networkRecord(id)
	if err != nil {
		return err
	}
	return a.syncPortsLocked(n, false)
}

// Sampling only repairs a known, previously applied binding whose address has
// changed. Failed or ambiguous writes stay pending for explicit operator retry.
func (a *App) refreshForwardAddress(id string, state *incusx.InstanceState) {
	n, err := a.networkRecord(id)
	if err != nil || n.Deleting || n.Uncertain || (n.Status != "ready" && n.Status != "inactive" && n.Status != "waiting-address") {
		return
	}
	target := a.managedGuestIPv4(state)
	stopped := state != nil && strings.EqualFold(state.Status, "Stopped")
	if stopped && n.Status == "inactive" {
		return
	}
	if !stopped && n.Status == "ready" && target == n.Target {
		return
	}
	if !stopped && n.Status != "ready" && target == "" {
		return
	}
	a.allocationMu.Lock()
	defer a.allocationMu.Unlock()
	current, err := a.mutableNetwork(id)
	if err == nil {
		_ = a.syncPortsLocked(current, false)
	}
}

func (a *App) cleanupForwardsLocked(n networkRecord) error {
	ports, err := a.instancePorts(n.ID)
	if err != nil {
		return err
	}
	grouped := map[string][]Port{}
	for _, p := range ports {
		grouped[p.ListenIP] = append(grouped[p.ListenIP], p)
	}
	for listen, group := range grouped {
		if err := a.updateInstanceForward(n, listen, "", group, true); err != nil {
			return err
		}
	}
	return a.flushConntrackCleanup(n.ID)
}

func recordNetworkPending(tx *sql.Tx, id string) error {
	_, err := tx.Exec(`INSERT INTO instance_network(instance_id,status,updated_at) VALUES(?,'pending',?)
		ON CONFLICT(instance_id) DO UPDATE SET status='pending',last_error='',updated_at=excluded.updated_at`, id, store.Now())
	return err
}
