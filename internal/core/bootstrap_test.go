package core

import (
	"os"
	"strings"
	"testing"
)

func TestBootstrapFileFailureDoesNotStrandAdministrator(t *testing.T) {
	a, _ := testApp(t)
	if err := os.Mkdir(a.Cfg.Bootstrap(), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BootstrapAdmin(); err == nil {
		t.Fatal("file failure was ignored")
	}
	exists, err := a.Auth.HasAdmin()
	if err != nil || exists {
		t.Fatal("administrator persisted without password delivery")
	}
	if err := os.Remove(a.Cfg.Bootstrap()); err != nil {
		t.Fatal(err)
	}
	password, err := a.BootstrapAdmin()
	if err != nil || password == "" || !a.Auth.CheckAdmin(password) {
		t.Fatal("retry did not recover")
	}
	info, err := os.Stat(a.Cfg.Bootstrap())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe bootstrap permissions")
	}
}
func TestBootstrapDatabaseFailureReusesDeliveredPassword(t *testing.T) {
	a, _ := testApp(t)
	_, err := a.Store.DB.Exec("CREATE TRIGGER fail_admin BEFORE INSERT ON admin BEGIN SELECT RAISE(ABORT,'injected'); END")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.BootstrapAdmin(); err == nil {
		t.Fatal("database failure ignored")
	}
	delivered, err := os.ReadFile(a.Cfg.Bootstrap())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.DB.Exec("DROP TRIGGER fail_admin"); err != nil {
		t.Fatal(err)
	}
	password, err := a.BootstrapAdmin()
	if err != nil || password != strings.TrimSuffix(string(delivered), "\n") || !a.Auth.CheckAdmin(password) {
		t.Fatal("retry changed the delivered password")
	}
	if result, err := a.BootstrapAdmin(); err != nil || result != "" {
		t.Fatal("existing administrator was reset")
	}
}
func TestBootstrapFailsClosedWhenAdministratorLookupFails(t *testing.T) {
	a, _ := testApp(t)
	if _, err := a.Store.DB.Exec("DROP TABLE admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BootstrapAdmin(); err == nil {
		t.Fatal("lookup failure was treated as first startup")
	}
	if _, err := os.Stat(a.Cfg.Bootstrap()); !os.IsNotExist(err) {
		t.Fatal("lookup failure created a password")
	}
}
