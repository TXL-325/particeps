package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAuthenticationMaintenancePreservesLiveState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_, err = s.DB.Exec("INSERT INTO sessions VALUES('expired',0,?),('live',0,?); INSERT INTO tasks(id,kind,request_hash,status,created_at,updated_at) VALUES('t','create','hash','ok',0,0)", now.Add(-time.Hour).Unix(), now.Add(time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	rows := []struct{ name, raw string }{
		{"expired", `{"credential":{"expiresAt":1,"ciphertext":"fixture"}}`},
		{"live", `{"credential":{"expiresAt":4102444800,"ciphertext":"fixture"}}`},
		{"other", `{"result":"unrelated task result"}`},
		{"invalid-expiry", `{"credential":{"expiresAt":"invalid","ciphertext":"fixture"}}`},
	}
	for _, row := range rows {
		if _, err := s.DB.Exec("INSERT INTO task_items(task_id,name,status,result_json) VALUES('t',?,'ok',?)", row.name, row.raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, row := range rows {
		var raw string
		if err := s.DB.QueryRow("SELECT result_json FROM task_items WHERE name=?", row.name).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		shouldClear := row.name == "expired" || row.name == "invalid-expiry"
		if (raw == "") != shouldClear {
			t.Errorf("unexpected retention for %s", row.name)
		}
	}
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatal("session expiry failed")
	}
	if err := s.PruneAuthentication(now.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal("periodic pruning failed")
	}
}
