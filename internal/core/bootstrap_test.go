package core

import (
	"os"
	"testing"
)

func TestBootstrapCreatesAdministratorWithoutPasswordFile(t *testing.T) {
	a, _ := testApp(t)
	t.Setenv("PARTICEPS_ADMIN_PASSWORD", "")
	password, err := a.BootstrapAdmin()
	if err != nil || password == "" || !a.Auth.CheckAdmin(password) {
		t.Fatal("administrator was not created")
	}
	if _, err := os.Stat(a.Cfg.Bootstrap()); !os.IsNotExist(err) {
		t.Fatal("password file was written")
	}
}

func TestBootstrapUsesEnvironmentPassword(t *testing.T) {
	a, _ := testApp(t)
	const want = "env-password-12345678"
	t.Setenv("PARTICEPS_ADMIN_PASSWORD", want)
	password, err := a.BootstrapAdmin()
	if err != nil || password != want || !a.Auth.CheckAdmin(want) {
		t.Fatal("environment password was not used")
	}
	if _, err := os.Stat(a.Cfg.Bootstrap()); !os.IsNotExist(err) {
		t.Fatal("password file was written")
	}
}

func TestBootstrapRejectsInvalidEnvironmentPassword(t *testing.T) {
	a, _ := testApp(t)
	t.Setenv("PARTICEPS_ADMIN_PASSWORD", "bad\npassword")
	if _, err := a.BootstrapAdmin(); err == nil {
		t.Fatal("invalid password was accepted")
	}
	exists, err := a.Auth.HasAdmin()
	if err != nil || exists {
		t.Fatal("administrator persisted after invalid password")
	}
}

func TestBootstrapDatabaseFailureDoesNotWritePasswordFile(t *testing.T) {
	a, _ := testApp(t)
	t.Setenv("PARTICEPS_ADMIN_PASSWORD", "retry-password-abcdef")
	_, err := a.Store.DB.Exec("CREATE TRIGGER fail_admin BEFORE INSERT ON admin BEGIN SELECT RAISE(ABORT,'injected'); END")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.BootstrapAdmin(); err == nil {
		t.Fatal("database failure ignored")
	}
	if _, err := os.Stat(a.Cfg.Bootstrap()); !os.IsNotExist(err) {
		t.Fatal("password file was written after database failure")
	}
	if _, err = a.Store.DB.Exec("DROP TRIGGER fail_admin"); err != nil {
		t.Fatal(err)
	}
	password, err := a.BootstrapAdmin()
	if err != nil || password != "retry-password-abcdef" || !a.Auth.CheckAdmin(password) {
		t.Fatal("retry did not use environment password")
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
		t.Fatal("lookup failure created a password file")
	}
}
