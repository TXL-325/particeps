package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.PruneAuthentication(time.Now()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS admin (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  password_hash BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS tokens (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  hash TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  revoked INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS settings (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS instances (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  incus_name TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  image TEXT NOT NULL,
  cpu_cores REAL NOT NULL,
  cpu_pin TEXT NOT NULL DEFAULT '',
  memory_mib INTEGER NOT NULL,
  disk_gib INTEGER NOT NULL,
  bandwidth_mbps INTEGER NOT NULL DEFAULT 0,
  stack_mode TEXT NOT NULL,
  desired_power TEXT NOT NULL,
  ssh_pubkey TEXT NOT NULL DEFAULT '',
  password_login INTEGER NOT NULL DEFAULT 1,
  nat_ipv4 TEXT NOT NULL DEFAULT '',
  dedicated_ipv4 TEXT NOT NULL DEFAULT '',
  ipv6 TEXT NOT NULL DEFAULT '',
  ipv6_mode TEXT NOT NULL DEFAULT '',
  source_ip_limit INTEGER,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS ports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  instance_id TEXT NOT NULL,
  number INTEGER NOT NULL,
  proto TEXT NOT NULL,
  listen_ip TEXT NOT NULL,
  target INTEGER NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  UNIQUE(number, proto, listen_ip),
  FOREIGN KEY(instance_id) REFERENCES instances(id)
);
CREATE TABLE IF NOT EXISTS resource_updates (
  instance_id TEXT PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
  request_json TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS password_updates (
  instance_id TEXT PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
  operation TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS instance_network (
  instance_id TEXT PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
  target_ipv4 TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  last_error TEXT NOT NULL DEFAULT '',
  deleting INTEGER NOT NULL DEFAULT 0,
  uncertain INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS forward_writes (
  network TEXT NOT NULL,
  listen_address TEXT NOT NULL,
  token TEXT NOT NULL UNIQUE,
  instance_id TEXT NOT NULL REFERENCES instances(id),
  phase TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(network,listen_address)
);
CREATE TABLE IF NOT EXISTS conntrack_cleanup (
  instance_id TEXT NOT NULL REFERENCES instances(id),
  listen_address TEXT NOT NULL,
  number INTEGER NOT NULL,
  proto TEXT NOT NULL,
  reply_address TEXT NOT NULL DEFAULT '',
  reply_port INTEGER NOT NULL DEFAULT 0,
  direct_binding INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(instance_id,listen_address,number,proto,reply_address,reply_port)
);
CREATE TRIGGER IF NOT EXISTS ports_single_owner_insert BEFORE INSERT ON ports
WHEN EXISTS (SELECT 1 FROM ports WHERE number=NEW.number AND instance_id!=NEW.instance_id)
BEGIN SELECT RAISE(ABORT,'port number already belongs to another instance'); END;
CREATE TRIGGER IF NOT EXISTS ports_single_owner_update BEFORE UPDATE OF number,instance_id ON ports
WHEN EXISTS (SELECT 1 FROM ports WHERE number=NEW.number AND instance_id!=NEW.instance_id)
BEGIN SELECT RAISE(ABORT,'port number already belongs to another instance'); END;
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  idempotency_key TEXT UNIQUE,
  request_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS task_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  instance_id TEXT,
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  step TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  result_json TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(task_id) REFERENCES tasks(id)
);
CREATE TABLE IF NOT EXISTS images (
  alias TEXT PRIMARY KEY,
  fingerprint TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL,
  registered INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS traffic (
  instance_id TEXT PRIMARY KEY,
  period_rx INTEGER NOT NULL DEFAULT 0,
  period_tx INTEGER NOT NULL DEFAULT 0,
  reset_at INTEGER NOT NULL
);
UPDATE task_items SET result_json='' WHERE CASE WHEN json_valid(result_json)
  THEN json_type(result_json,'$.password') IS NOT NULL ELSE 0 END;
`)
	if err != nil {
		return err
	}
	// Keep previously applied mappings enabled when upgrading existing ledgers.
	if err := s.ensureColumn("ports", "enabled", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	// Early development databases may already contain the cleanup queue.
	return s.ensureColumn("conntrack_cleanup", "direct_binding", "INTEGER NOT NULL DEFAULT 0")
}

// The table, column and definition arguments are schema constants, never input.
func (s *Store) ensureColumn(table, column, definition string) error {
	rows, err := s.DB.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, required, primary int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &required, &defaultValue, &primary); err != nil {
			_ = rows.Close()
			return err
		}
		found = found || name == column
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if !found {
		_, err = s.DB.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	}
	return err
}

func (s *Store) Setting(k, def string) string {
	var v string
	err := s.DB.QueryRow(`SELECT v FROM settings WHERE k=?`, k).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(k, v string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

func (s *Store) JSONSetting(k string, dest any) error {
	v := s.Setting(k, "")
	if v == "" {
		return sql.ErrNoRows
	}
	return json.Unmarshal([]byte(v), dest)
}

func (s *Store) SetJSONSetting(k string, val any) error {
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return s.SetSetting(k, string(b))
}

func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func Now() int64 { return time.Now().Unix() }

func (s *Store) NextPorts(n, start, end int) ([]int, error) {
	return s.NextPortsExcluding(n, start, end, nil)
}

func (s *Store) NextPortsExcluding(n, start, end int, excluded map[int]bool) ([]int, error) {
	if n < 1 || start < 1 || end > 65535 || start > end || n > end-start+1 {
		return nil, fmt.Errorf("invalid port pool or allocation size")
	}
	rows, err := s.DB.Query(`SELECT DISTINCT number FROM ports`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := map[int]bool{}
	for number, blocked := range excluded {
		used[number] = blocked
	}
	for rows.Next() {
		var num int
		if err := rows.Scan(&num); err != nil {
			return nil, err
		}
		used[num] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]int, 0, n)
	for p := start; p <= end && len(out) < n; p++ {
		if used[p] {
			out = out[:0]
			continue
		}
		out = append(out, p)
	}
	if len(out) < n {
		return nil, fmt.Errorf("port pool exhausted")
	}
	return out, nil
}
