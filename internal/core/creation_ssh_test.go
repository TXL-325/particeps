package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"particeps/internal/incusx"
	"particeps/internal/store"
)

type creationSSHBackend struct {
	*forwardBackend
	failure       string
	failureError  error
	beforeStep    func(string)
	password      string
	passwordLogin bool
}

func (b *creationSSHBackend) CreateInstance(name, image string, config map[string]string, devices map[string]map[string]string) error {
	b.events = append(b.events, "create")
	b.addresses[name] = "10.80.0.20"
	return b.resourceBackend.CreateInstance(name, image, config, devices)
}

func (b *creationSSHBackend) sshStep(step string) error {
	if b.beforeStep != nil {
		b.beforeStep(step)
	}
	b.events = append(b.events, step)
	if b.failure == step {
		if b.failureError != nil {
			return b.failureError
		}
		return &incusx.ExecError{Operation: incusx.ConfigOperation{Terminal: true}, Cause: errors.New("injected " + step + " failure")}
	}
	return nil
}

func (b *creationSSHBackend) PrepareSSH(_ string, passwordLogin bool) error {
	b.passwordLogin = passwordLogin
	return b.sshStep("prepare-ssh")
}

func (b *creationSSHBackend) SetRootPassword(_, password string) error {
	if err := b.sshStep("password"); err != nil {
		return err
	}
	b.password = password
	return nil
}

func (b *creationSSHBackend) InstallRootKey(_, _ string) error { return b.sshStep("key") }

func (b *creationSSHBackend) StartSSH(_ string, passwordLogin bool) error {
	if passwordLogin != b.passwordLogin || b.password == "" {
		return errors.New("activation ran before credentials or changed login policy")
	}
	return b.sshStep("start-ssh")
}

func sshCreationApp(t *testing.T) (*App, *creationSSHBackend) {
	t.Helper()
	a, base := networkTestApp(t)
	b := &creationSSHBackend{forwardBackend: base}
	a.Incus = b
	if err := a.SetPool(PoolSettings{IPv4: []string{"192.0.2.1"}, NATIPv4: "192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.DB.Exec(`INSERT INTO tasks(id,kind,request_hash,status,created_at,updated_at) VALUES('ssh-create','create','h','running',0,0);
		INSERT INTO task_items(task_id,name,status,step) VALUES('ssh-create','new-ssh-guest','pending','queued')`); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func TestCreationWaitsForSSHBeforePublishingForwards(t *testing.T) {
	for _, passwordLogin := range []bool{true, false} {
		name := "password"
		if !passwordLogin {
			name = "publickey-only"
		}
		t.Run(name, func(t *testing.T) {
			a, b := sshCreationApp(t)
			req := CreateReq{Image: "alpine/3.21/cloud", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4",
				PasswordLogin: &passwordLogin, Password: "fixture-request-password", SSHPubKey: "ssh-ed25519 fixture-public-key"}
			a.runCreateItem("ssh-create", "new-ssh-guest", req)
			task, err := a.GetTask("ssh-create")
			if err != nil || task.Status != "ok" || len(task.Items) != 1 || task.Items[0].Step != "done" {
				t.Fatalf("creation did not finish: %+v %v", task, err)
			}
			want := []string{"create", "start", "prepare-ssh", "password", "key", "start-ssh", "forward"}
			if !reflect.DeepEqual(b.events, want) {
				t.Fatalf("credential/forward sequence: %v", b.events)
			}
			n, err := a.networkRecord(task.Items[0].InstanceID)
			if err != nil || n.Uncertain || n.Status != "ready" {
				t.Fatalf("confirmed SSH completion did not release its barrier: %+v %v", n, err)
			}
			if task.Items[0].CredentialAvailable != passwordLogin {
				t.Fatal("initial password delivery ignored the requested login mode")
			}
			if passwordLogin {
				if b.password != req.Password {
					t.Fatal("the requested password was replaced")
				}
			} else {
				if len(b.password) < 32 || b.password == req.Password {
					t.Fatal("public-key root account was not unlocked with a private random value")
				}
				var result string
				if err := a.Store.DB.QueryRow(`SELECT result_json FROM task_items WHERE task_id='ssh-create'`).Scan(&result); err != nil || result != "" {
					t.Fatal("the public-key-only unlock value was persisted for delivery")
				}
			}
			var storedMode bool
			if err := a.Store.DB.QueryRow(`SELECT password_login FROM instances WHERE name='new-ssh-guest'`).Scan(&storedMode); err != nil || storedMode != passwordLogin {
				t.Fatal("stored login policy disagrees with SSH policy")
			}
		})
	}
}

func TestSSHProvisioningFailureFailsTaskWithoutForwardsOrCredentials(t *testing.T) {
	for _, failure := range []string{"prepare-ssh", "password", "key", "start-ssh"} {
		t.Run(failure, func(t *testing.T) {
			a, b := sshCreationApp(t)
			b.failure = failure
			a.runCreateItem("ssh-create", "new-ssh-guest", CreateReq{
				Image: "alpine/3.21/cloud", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4", SSHPubKey: "ssh-ed25519 fixture-public-key",
			})
			task, err := a.GetTask("ssh-create")
			if err != nil || task.Status != "failed" || len(task.Items) != 1 || !strings.Contains(task.Items[0].Error, failure) {
				t.Fatalf("provisioning failure was hidden: %+v %v", task, err)
			}
			if task.Items[0].CredentialAvailable || b.writes != 0 {
				t.Fatal("failed SSH provisioning exposed a password or installed forwarding")
			}
			if b.events[len(b.events)-1] != failure {
				t.Fatalf("creation continued after %s: %v", failure, b.events)
			}
			wantStep := "ssh"
			if failure == "password" {
				wantStep = "password"
			}
			if task.Items[0].Step != wantStep {
				t.Fatalf("wrong failure step: %s", task.Items[0].Step)
			}
			n, err := a.networkRecord(task.Items[0].InstanceID)
			if err != nil || n.Uncertain {
				t.Fatalf("a terminal SSH failure retained an uncertain operation: %+v %v", n, err)
			}
			if err := a.DeleteInstance(n.ID); err != nil || b.deletes != 1 {
				t.Fatalf("a terminal SSH failure could not be explicitly cleaned up: %v", err)
			}
		})
	}
}

func TestUncertainSSHProvisioningSurvivesRestartAndBlocksMutations(t *testing.T) {
	for _, phase := range []string{"prepare-ssh", "start-ssh"} {
		for _, reference := range []string{"", "/1.0/operations/ssh-fixture?secret=fixture-query-secret"} {
			name := phase + "/without-reference"
			if reference != "" {
				name = phase + "/with-reference"
			}
			t.Run(name, func(t *testing.T) {
				a, b := sshCreationApp(t)
				b.failure, b.failureError = phase, &incusx.ExecError{
					Operation: incusx.ConfigOperation{ID: reference}, Cause: errors.New("fixture-sensitive-transport-diagnostic"),
				}
				b.beforeStep = func(step string) {
					if step != phase {
						return
					}
					n, err := a.networkRecord("new-ssh-guest")
					var message string
					readErr := a.Store.DB.QueryRow(`SELECT last_error FROM instance_network WHERE instance_id=?`, n.ID).Scan(&message)
					if err != nil || readErr != nil || !n.Uncertain || n.Status != "needs-reconciliation" || !strings.Contains(message, phase) {
						t.Fatalf("SSH dispatched without a durable phase/barrier: %+v %q %v %v", n, message, err, readErr)
					}
				}
				a.runCreateItem("ssh-create", "new-ssh-guest", CreateReq{
					Image: "alpine/3.21/cloud", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4",
					Password: "fixture-request-password", SSHPubKey: "ssh-ed25519 fixture-public-key",
				})
				task, err := a.GetTask("ssh-create")
				if err != nil || task.Status != "failed" || len(task.Items) != 1 || task.Items[0].Step != "ssh" || task.Items[0].CredentialAvailable {
					t.Fatalf("unconfirmed SSH execution did not fail closed: %+v %v", task, err)
				}
				id := task.Items[0].InstanceID
				b.beforeStep = nil
				if err := a.Store.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := store.Open(a.Cfg.StateDB())
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				a = &App{Cfg: a.Cfg, Store: reopened, Metrics: a.Metrics, Incus: b}
				n, err := a.networkRecord(id)
				var message string
				readErr := a.Store.DB.QueryRow(`SELECT last_error FROM instance_network WHERE instance_id=?`, id).Scan(&message)
				if err != nil || readErr != nil || !n.Uncertain || n.Status != "needs-reconciliation" || !strings.Contains(message, phase) {
					t.Fatalf("restart lost SSH reconciliation state: %+v %q %v %v", n, message, err, readErr)
				}
				for _, text := range []string{message, task.Items[0].Error} {
					if !strings.Contains(text, "manual reconciliation") || !strings.Contains(text, phase) {
						t.Fatalf("unknown outcome has no actionable phase: %q", text)
					}
					if reference != "" && !strings.Contains(text, "/1.0/operations/ssh-fixture") {
						t.Fatalf("operation reference was lost: %q", text)
					}
					for _, secret := range []string{"fixture-sensitive-transport-diagnostic", "fixture-query-secret", "fixture-request-password", "fixture-public-key"} {
						if strings.Contains(text, secret) {
							t.Fatal("SSH reconciliation diagnostics exposed sensitive input")
						}
					}
				}
				before := append([]string(nil), b.events...)
				for _, action := range []struct {
					name string
					run  func() error
				}{
					{"sync", func() error { return a.SyncPorts(id) }},
					{"start", func() error { return a.Power(id, "start", false) }},
					{"stop", func() error { return a.Power(id, "stop", false) }},
					{"restart", func() error { return a.Power(id, "restart", false) }},
					{"delete", func() error { return a.DeleteInstance(id) }},
					{"password", func() error { _, err := a.ResetPassword(id, "fixture-replacement"); return err }},
				} {
					if err := action.run(); err == nil || !strings.Contains(err.Error(), "reconciliation") {
						t.Fatalf("%s bypassed an unconfirmed SSH operation: %v", action.name, err)
					}
				}
				if !reflect.DeepEqual(b.events, before) || b.writes != 0 || b.deletes != 0 {
					t.Fatalf("blocked operations reached the backend: %v", b.events)
				}
				n, err = a.networkRecord(id)
				if err != nil || !n.Uncertain || n.Status != "needs-reconciliation" {
					t.Fatalf("rejected mutation cleared the SSH barrier: %+v %v", n, err)
				}
			})
		}
	}
}

func TestSSHProvisioningJournalFailurePreventsDispatch(t *testing.T) {
	a, b := sshCreationApp(t)
	if _, err := a.Store.DB.Exec(`CREATE TRIGGER fail_ssh_journal BEFORE INSERT ON instance_network
		WHEN NEW.uncertain=1 BEGIN SELECT RAISE(ABORT,'injected journal failure'); END`); err != nil {
		t.Fatal(err)
	}
	a.runCreateItem("ssh-create", "new-ssh-guest", CreateReq{
		Image: "alpine/3.21/cloud", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4",
	})
	task, err := a.GetTask("ssh-create")
	if err != nil || task.Status != "failed" || len(task.Items) != 1 || task.Items[0].Step != "ssh" {
		t.Fatalf("missing journal did not stop SSH provisioning: %+v %v", task, err)
	}
	if !reflect.DeepEqual(b.events, []string{"create", "start"}) || b.writes != 0 {
		t.Fatalf("SSH was dispatched without its durable barrier: %v", b.events)
	}
}

func TestSSHProvisioningCompletionWriteFailureRetainsBarrier(t *testing.T) {
	a, b := sshCreationApp(t)
	b.beforeStep = func(step string) {
		if step == "prepare-ssh" {
			if _, err := a.Store.DB.Exec(`CREATE TRIGGER fail_ssh_completion BEFORE UPDATE OF uncertain ON instance_network
				WHEN NEW.uncertain=0 BEGIN SELECT RAISE(ABORT,'injected completion failure'); END`); err != nil {
				t.Fatal(err)
			}
		}
	}
	a.runCreateItem("ssh-create", "new-ssh-guest", CreateReq{
		Image: "alpine/3.21/cloud", CPUCores: 0.5, MemoryMiB: 128, DiskGiB: 1, StackMode: "v4",
	})
	task, err := a.GetTask("ssh-create")
	if err != nil || task.Status != "failed" || len(task.Items) != 1 || !strings.Contains(task.Items[0].Error, "completion could not be saved") {
		t.Fatalf("completion persistence failure was hidden: %+v %v", task, err)
	}
	n, err := a.networkRecord(task.Items[0].InstanceID)
	if err != nil || !n.Uncertain || n.Status != "needs-reconciliation" || b.writes != 0 {
		t.Fatalf("completion persistence failure lost its barrier: %+v %v", n, err)
	}
	if !reflect.DeepEqual(b.events, []string{"create", "start", "prepare-ssh"}) {
		t.Fatalf("creation continued without a durable SSH completion: %v", b.events)
	}
}
