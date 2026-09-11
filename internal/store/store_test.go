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
