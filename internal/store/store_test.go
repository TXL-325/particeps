package store

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestNextPortsFindsContiguousBlock(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.DB.Exec(`INSERT INTO instances(id,name,incus_name,display_name,image,cpu_cores,memory_mib,disk_gib,stack_mode,desired_power,created_at)
		VALUES('existing','existing','existing','existing','alpine',0.5,128,1,'v4','running',0)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`INSERT INTO ports(instance_id,number,proto,listen_ip,target) VALUES('existing',20001,'tcp','192.0.2.1',22)`)
	if err != nil {
		t.Fatal(err)
	}
	ports, err := s.NextPorts(3, 20000, 20005)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ports, []int{20002, 20003, 20004}) {
		t.Fatalf("fragmented allocation: %v", ports)
	}
	if ports, err := s.NextPorts(4, 20000, 20004); err == nil {
		t.Fatalf("accepted non-contiguous block: %v", ports)
	}
}

func TestCleanupQueueMigrationKeepsPendingDNATEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`DROP TABLE conntrack_cleanup;
CREATE TABLE conntrack_cleanup(instance_id TEXT,listen_address TEXT,number INTEGER,proto TEXT,reply_address TEXT,reply_port INTEGER,PRIMARY KEY(instance_id,listen_address,number,proto,reply_address,reply_port));
INSERT INTO conntrack_cleanup VALUES('guest','192.0.2.1',22000,'udp','10.80.0.10',8080);`)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var direct int
	var address string
	if err := s.DB.QueryRow(`SELECT reply_address,direct_binding FROM conntrack_cleanup`).Scan(&address, &direct); err != nil {
		t.Fatal(err)
	}
	if address != "10.80.0.10" || direct != 0 {
		t.Fatalf("migration changed a pending cleanup: %s %d", address, direct)
	}
}

func TestPortEnabledMigrationPreservesLegacyMappingsAndNewDisabledRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`
INSERT INTO instances(id,name,incus_name,display_name,image,cpu_cores,memory_mib,disk_gib,stack_mode,desired_power,created_at)
VALUES('guest','guest','p-guest','guest','alpine',0.5,128,1,'v4','running',0);
ALTER TABLE ports DROP COLUMN enabled;
INSERT INTO ports(instance_id,number,proto,listen_ip,target) VALUES
('guest',20000,'tcp','192.0.2.1',22),
('guest',20000,'udp','192.0.2.1',20000),
('guest',20001,'tcp','192.0.2.1',8080);`)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var enabled, count, target int
	if err := s.DB.QueryRow(`SELECT SUM(enabled),COUNT(*) FROM ports`).Scan(&enabled, &count); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT target FROM ports WHERE number=20001 AND proto='tcp'`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if enabled != 3 || count != 3 || target != 8080 {
		t.Fatalf("upgrade disabled or changed existing mappings: enabled=%d count=%d target=%d", enabled, count, target)
	}
	if _, err := s.DB.Exec(`UPDATE ports SET enabled=0 WHERE number=20001`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.DB.QueryRow(`SELECT enabled,target FROM ports WHERE number=20001`).Scan(&enabled, &target); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 || target != 8080 {
		t.Fatal("reopening the database re-enabled a reserved port or lost its target")
	}
	next, err := s.NextPorts(1, 20000, 20003)
	if err != nil || !reflect.DeepEqual(next, []int{20002}) {
		t.Fatalf("migration released a disabled reservation: %v %v", next, err)
	}
}
