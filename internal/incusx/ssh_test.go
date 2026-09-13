package incusx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCloudInitStatusDistinguishesWarningsFromFailure(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		done, fails  bool
	}{
		{"done", `{"exit_code":0,"result":{"status":"done","errors":[]}}`, true, false},
		{"alpine-missing-ssh-warning", `{"exit_code":2,"result":{"status":"done","extended_status":"degraded done","errors":[],"recoverable_errors":{"WARNING":["sshd is not installed"]}}}`, true, false},
		{"not-started", `{"exit_code":0,"result":{"status":"not started","extended_status":"not started","errors":[],"recoverable_errors":{}}}`, false, false},
		{"legacy-not-run", `{"exit_code":0,"result":{"status":"not run"}}`, false, false},
		{"running", `{"exit_code":0,"result":{"status":"running"}}`, false, false},
		{"terminal-error", `{"exit_code":1,"result":{"status":"error","errors":["fixture-secret"]}}`, false, true},
		{"done-with-fatal-error", `{"exit_code":2,"result":{"status":"done","errors":["fixture-secret"]}}`, false, true},
		{"nested-module-error", `{"exit_code":0,"result":{"status":"done","modules-final":{"errors":["fixture-secret"]}}}`, false, true},
		{"failed-extended-status", `{"exit_code":0,"result":{"status":"done","extended_status":"failed done"}}`, false, true},
		{"disabled", `{"exit_code":0,"result":{"status":"disabled"}}`, false, true},
		{"command-failed", `{"exit_code":127,"result":{"status":"done"}}`, false, true},
		{"missing-exit", `{"result":{"status":"done"}}`, false, true},
		{"invalid-json", `fixture-secret`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done, err := cloudInitDone(tc.status)
			if done != tc.done || (err != nil) != tc.fails {
				t.Fatalf("done=%t error=%v", done, err)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("cloud-init user data leaked into the task error")
			}
		})
	}
}

func TestCloudInitWaitsUntilDoneAndHonorsCancellation(t *testing.T) {
	sequence := []string{"not started", "running", "done"}
	calls := 0
	err := waitCloudInit(context.Background(), 0, func(context.Context) (string, error) {
		status := sequence[calls]
		calls++
		return fmt.Sprintf(`{"exit_code":0,"result":{"status":%q}}`, status), nil
	})
	if err != nil || calls != len(sequence) {
		t.Fatalf("cloud-init was not awaited: calls=%d error=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls = 0
	err = waitCloudInit(ctx, time.Hour, func(context.Context) (string, error) {
		calls++
		cancel()
		return `{"exit_code":0,"result":{"status":"running"}}`, nil
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || !ExecOperationState(err).Terminal {
		t.Fatalf("cloud-init wait ignored cancellation: calls=%d error=%v", calls, err)
	}
}

func TestCloudInitWaitPreservesUnconfirmedExec(t *testing.T) {
	for _, reference := range []string{"", "/1.0/operations/cloud-init-fixture"} {
		execution := &ExecError{Operation: ConfigOperation{ID: reference}, Cause: context.DeadlineExceeded}
		err := waitCloudInit(context.Background(), 0, func(context.Context) (string, error) {
			return "", execution
		})
		var got *ExecError
		if !errors.As(err, &got) || got != execution || ExecOperationState(err).Terminal {
			t.Fatalf("cloud-init classified an unconfirmed exec as completed: %v", err)
		}
	}
}

func TestSSHProvisioningPreservesBackendExecState(t *testing.T) {
	for _, method := range []string{"prepare", "start"} {
		for _, tc := range []struct {
			name, response, reference string
			terminal                  bool
		}{
			{"lost-response", `incomplete-response`, "", false},
			{"unconfirmed-operation", `{"type":"async","operation":"/1.0/operations/ssh-fixture"}`, "/1.0/operations/ssh-fixture", false},
			{"rejected-operation", `{"type":"error","error_code":400,"error":"fixture rejection"}`, "", true},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost {
						t.Errorf("unexpected follow-up after rejected or unconfirmed dispatch: %s", r.URL.Path)
					}
					fmt.Fprint(w, tc.response)
				})
				var err error
				if method == "prepare" {
					err = client.PrepareSSH("guest", true)
				} else {
					err = client.StartSSH("guest", true)
				}
				operation := ExecOperationState(err)
				if err == nil || operation.ID != tc.reference || operation.Terminal != tc.terminal {
					t.Fatalf("SSH provisioning lost exec state: %+v %v", operation, err)
				}
			})
		}
	}
}

func TestSSHLocalValidationFailureHasKnownExecCompletion(t *testing.T) {
	const done = `{"exit_code":0,"result":{"status":"done"}}`
	const passwordPolicy = "port 22\npermitrootlogin yes\npasswordauthentication yes\npubkeyauthentication yes\nkbdinteractiveauthentication no\npermitemptypasswords no\nauthenticationmethods any\n"
	for _, tc := range []struct {
		name, method string
		outputs      []string
	}{
		{"cloud-init-json", "prepare", []string{"fixture-private-output"}},
		{"cloud-init-status", "prepare", []string{`{"exit_code":1,"result":{"status":"error","errors":["fixture-private-output"]}}`}},
		{"prepare-policy", "prepare", []string{done, "fixture-private-output"}},
		{"login-policy", "start", []string{"fixture-private-output"}},
		{"activation-policy", "start", []string{passwordPolicy, "fixture-private-output"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upgrader := websocket.Upgrader{}
			// Successful execs supply controlled output. No guest commands run.
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					call := calls.Add(1)
					if int(call) > len(tc.outputs) {
						t.Error("provisioning continued after a local validation failure")
						http.Error(w, "unexpected exec", http.StatusBadRequest)
						return
					}
					fmt.Fprintf(w, `{"type":"async","operation":"/1.0/operations/%d","metadata":{"metadata":{"fds":{"0":"stdin","1":"stdout","2":"stderr"}}}}`, call)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/wait") {
					operationResponse(w, 200, "", map[string]any{"return": 0})
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/websocket") {
					http.NotFound(w, r)
					return
				}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				if r.URL.Query().Get("secret") == "stdin" {
					_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
					for {
						kind, data, err := conn.ReadMessage()
						if err != nil || (kind == websocket.TextMessage && len(data) == 0) {
							return
						}
					}
				}
				if r.URL.Query().Get("secret") == "stdout" {
					call, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/1.0/operations/"), "/websocket"))
					if err != nil || call < 1 || call > len(tc.outputs) {
						t.Errorf("invalid fixture exec: %s", r.URL.Path)
						return
					}
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte(tc.outputs[call-1]))
				}
				_ = conn.WriteMessage(websocket.TextMessage, nil)
			})
			var err error
			if tc.method == "prepare" {
				err = client.PrepareSSH("guest", true)
			} else {
				err = client.StartSSH("guest", true)
			}
			if err == nil || !ExecOperationState(err).Terminal || int(calls.Load()) != len(tc.outputs) {
				t.Fatalf("local validation retained an uncertain exec: calls=%d state=%+v error=%v", calls.Load(), ExecOperationState(err), err)
			}
			if strings.Contains(err.Error(), "fixture-private-output") {
				t.Fatal("local validation exposed guest output")
			}
		})
	}
}

func TestSSHProvisioningExecCancellationRetainsOperation(t *testing.T) {
	waiting := make(chan struct{})
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"type":"async","operation":"/1.0/operations/ssh-provisioning"}`)
			return
		}
		close(waiting)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.execContext(ctx, "guest", []string{"true"}, nil)
		done <- err
	}()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("exec did not reach operation wait")
	}
	cancel()
	select {
	case err := <-done:
		operation := ExecOperationState(err)
		if !errors.Is(err, context.Canceled) || operation.Terminal || operation.ID != "/1.0/operations/ssh-provisioning" {
			t.Fatalf("cancellation lost an unconfirmed operation: %+v %v", operation, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("exec ignored cancellation")
	}
}

func TestSSHReadbackRejectsWeakenedOrIncompletePolicy(t *testing.T) {
	const valid = "port 22\npermitrootlogin without-password\npasswordauthentication no\npubkeyauthentication yes\nkbdinteractiveauthentication no\npermitemptypasswords no\nauthenticationmethods any\n"
	if err := checkSSHPolicy(valid, false, true); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{
		strings.ReplaceAll(valid, "passwordauthentication no", "passwordauthentication yes"),
		strings.ReplaceAll(valid, "kbdinteractiveauthentication no", "kbdinteractiveauthentication yes"),
		strings.ReplaceAll(valid, "permitemptypasswords no", "permitemptypasswords yes"),
		strings.ReplaceAll(valid, "pubkeyauthentication yes", "pubkeyauthentication no"),
		strings.ReplaceAll(valid, "permitrootlogin without-password", "permitrootlogin no"),
		strings.ReplaceAll(valid, "port 22", "port 2222"),
		valid + "port 2222\n", "",
	} {
		if err := checkSSHPolicy(changed, false, true); err == nil {
			t.Fatal("an unusable or weakened public-key-only policy was accepted")
		}
	}
}

func TestSSHProvisioningShellSyntax(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell is unavailable")
	}
	for name, script := range map[string]string{
		"cloud-init": cloudInitStatusScript,
		"prepare":    sshPrepareScript + sshPolicyScript(true, false) + sshReadPolicyScript,
		"password":   sshShellPreamble + sshPolicyScript(true, true) + sshReadPolicyScript,
		"publickey":  sshShellPreamble + sshPolicyScript(false, true) + sshReadPolicyScript,
		"activate":   sshActivateScript + sshReadPolicyScript,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(shell, "-n")
			cmd.Stdin = strings.NewReader(script)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("invalid shell syntax: %v %s", err, output)
			}
		})
	}
}

// These shell fixtures execute the production checks against disposable files;
// they do not access real shadow entries, networking, packages, or services.
func runSSHFixture(t *testing.T, script string, args ...string) error {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires a Linux POSIX shell")
	}
	cmd := exec.Command("sh", append([]string{"-s", "--"}, args...)...)
	cmd.Stdin = strings.NewReader(sshShellPreamble + script)
	output, err := cmd.CombinedOutput()
	if len(output) != 0 {
		t.Fatalf("credential/readiness check emitted unexpected output: %s", output)
	}
	return err
}

func TestSSHRootCheckRejectsEmptyAndLockedAccounts(t *testing.T) {
	for _, tc := range []struct {
		name, shadow string
		ready        bool
	}{
		{"hashed", "root:$6$fixture-hash:1:0:99999:7:::\n", true},
		{"locked", "root:!$6$fixture-hash:1:0:99999:7:::\n", false},
		{"uninitialized", "root:*:1:0:99999:7:::\n", false},
		{"empty", "root::1:0:99999:7:::\n", false},
		{"no-root", "other:$6$fixture-hash:1:0:99999:7:::\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shadow")
			if err := os.WriteFile(path, []byte(tc.shadow), 0600); err != nil {
				t.Fatal(err)
			}
			script := strings.ReplaceAll(sshRootCredentialScript, "/etc/shadow", "'"+strings.ReplaceAll(path, "'", "'\"'\"'")+"'")
			if err := runSSHFixture(t, script, "password"); (err == nil) != tc.ready {
				t.Fatalf("root readiness=%t, error=%v", tc.ready, err)
			}
		})
	}
}

func TestSSHListenerCheckRequiresNonLoopbackListeningPort22(t *testing.T) {
	for _, tc := range []struct {
		name, address, state string
		ready                bool
	}{
		{"wildcard", "00000000:0016", "0A", true},
		{"guest-ipv4", "1400500A:0016", "0A", true},
		{"loopback", "0100007F:0016", "0A", false},
		{"other-loopback", "0200007F:0016", "0A", false},
		{"ipv6-wildcard", "00000000000000000000000000000000:0016", "0A", true},
		{"ipv6-loopback", "00000000000000000000000001000000:0016", "0A", false},
		{"mapped-loopback", "0000000000000000FFFF00000100007F:0016", "0A", false},
		{"wrong-port", "00000000:0050", "0A", false},
		{"not-listening", "00000000:0016", "01", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"tcp", "tcp6"} {
				contents := fmt.Sprintf("0: %s 00000000:0000 %s 0:0 0:0 0 0 0\n", tc.address, tc.state)
				if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			script := "sleep() { :; }\n" + sshListenScript
			for _, name := range []string{"tcp6", "tcp"} {
				quoted := "'" + strings.ReplaceAll(filepath.Join(dir, name), "'", "'\"'\"'") + "'"
				script = strings.ReplaceAll(script, "/proc/net/"+name, quoted)
			}
			if err := runSSHFixture(t, script); (err == nil) != tc.ready {
				t.Fatalf("listener readiness=%t, error=%v", tc.ready, err)
			}
		})
	}
}

func TestSSHManagedIncludeUsesEffectivePolicyWithRealSSHD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Linux OpenSSH")
	}
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		sshd = "/usr/sbin/sshd"
		if _, err := os.Stat(sshd); err != nil {
			t.Skip("OpenSSH server is unavailable")
		}
	}
	if os.Geteuid() == 0 {
		if _, err := os.Stat("/run/sshd"); err != nil {
			t.Skip("sshd -T requires the host's existing /run/sshd directory")
		}
	}
	for _, mode := range []struct {
		name                  string
		passwordLogin, active bool
	}{
		{"staged", true, false}, {"password", true, true}, {"publickey-only", false, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			if strings.ContainsAny(dir, " '\t\r\n") {
				t.Skip("shell config fixture requires a simple temporary path")
			}
			config := filepath.Join(dir, "sshd_config")
			if err := os.WriteFile(config, []byte("PasswordAuthentication no\nPermitRootLogin no\n"), 0600); err != nil {
				t.Fatal(err)
			}
			// Run the actual include/policy writes twice to exercise an existing
			// cloud stub and idempotent replacement without changing /etc/ssh.
			script := strings.ReplaceAll(sshIncludeScript+sshPolicyScript(mode.passwordLogin, mode.active), "/etc/ssh", dir)
			for i := 0; i < 2; i++ {
				if err := runSSHFixture(t, script); err != nil {
					t.Fatal(err)
				}
			}
			contents, err := os.ReadFile(config)
			if err != nil || strings.Count(string(contents), "Include "+dir+"/particeps-root.conf") != 1 {
				t.Fatal("managed include was duplicated or lost")
			}
			hostKey := filepath.Join(dir, "host_ed25519")
			if output, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", hostKey).CombinedOutput(); err != nil {
				t.Fatalf("fixture host key: %v %s", err, output)
			}
			output, err := exec.Command(sshd, "-T", "-f", config, "-h", hostKey, "-C", "user=root,host=particeps,addr=192.0.2.1").CombinedOutput()
			if err != nil {
				t.Fatalf("sshd rejected fixture configuration: %v %s", err, output)
			}
			if err := checkSSHPolicy(string(output), mode.passwordLogin, mode.active); err != nil {
				t.Fatal(err)
			}
		})
	}
}
