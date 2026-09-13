package incusx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PrepareSSH waits out cloud-init before making changes it could overwrite.
// Services stay stopped and root SSH login stays disabled until StartSSH runs
// after the caller has installed the requested credentials.
func (c *Client) PrepareSSH(name string, passwordLogin bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := waitCloudInit(ctx, time.Second, func(ctx context.Context) (string, error) {
		return c.sshScript(ctx, name, 15, cloudInitStatusScript)
	}); err != nil {
		return err
	}
	ctx, stop := context.WithTimeout(context.Background(), 320*time.Second)
	defer stop()
	output, err := c.sshScript(ctx, name, 300, sshPrepareScript+sshPolicyScript(passwordLogin, false)+sshReadPolicyScript)
	if err != nil {
		return fmt.Errorf("SSH package/configuration preparation failed: %w", err)
	}
	return completedSSHCheck(checkSSHPolicy(output, passwordLogin, false))
}

// StartSSH activates login only after credentials exist, and checks both the
// effective root policy and a running service listening on a non-loopback :22.
func (c *Client) StartSSH(name string, passwordLogin bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := c.sshScript(ctx, name, 15, sshShellPreamble+sshPolicyScript(passwordLogin, true)+sshReadPolicyScript)
	if err != nil {
		return fmt.Errorf("SSH login configuration failed: %w", err)
	}
	if err := checkSSHPolicy(output, passwordLogin, true); err != nil {
		return completedSSHCheck(err)
	}
	mode := "password"
	if !passwordLogin {
		mode = "publickey"
	}
	output, err = c.sshScript(ctx, name, 35, sshActivateScript+sshReadPolicyScript, mode)
	if err != nil {
		return fmt.Errorf("SSH service/root-account readiness check failed: %w", err)
	}
	return completedSSHCheck(checkSSHPolicy(output, passwordLogin, true))
}

// Only local validation and waits between completed execs use this wrapper.
// An error returned by sshScript must retain its original exec operation state.
func completedSSHCheck(err error) error {
	if err == nil {
		return nil
	}
	return &ExecError{Operation: ConfigOperation{Terminal: true}, Cause: err}
}

func (c *Client) sshScript(ctx context.Context, name string, seconds int, script string, args ...string) (string, error) {
	// A guest-side timeout also bounds the command if the Agent or its transport
	// disappears. No credentials are placed in command arguments or scripts.
	command := []string{"timeout", "-k", "5", strconv.Itoa(seconds), "sh", "-s", "--"}
	command = append(command, args...)
	return c.execContext(ctx, name, command, []byte(script))
}

func waitCloudInit(ctx context.Context, interval time.Duration, inspect func(context.Context) (string, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return completedSSHCheck(fmt.Errorf("waiting for cloud-init: %w", err))
		}
		output, err := inspect(ctx)
		if err != nil {
			return fmt.Errorf("reading cloud-init status: %w", err)
		}
		done, err := cloudInitDone(output)
		if err != nil || done {
			return completedSSHCheck(err)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return completedSSHCheck(fmt.Errorf("waiting for cloud-init: %w", ctx.Err()))
		case <-timer.C:
		}
	}
}

func cloudInitDone(output string) (bool, error) {
	var response struct {
		ExitCode *int           `json:"exit_code"`
		Result   map[string]any `json:"result"`
	}
	if json.Unmarshal([]byte(output), &response) != nil || response.ExitCode == nil {
		return false, fmt.Errorf("cloud-init returned an invalid status")
	}
	status, _ := response.Result["status"].(string)
	extended, _ := response.Result["extended_status"].(string)
	if cloudInitErrors(response.Result) || strings.Contains(extended, "error") || strings.Contains(extended, "failed") {
		// cloud-init diagnostics may include user data. Keep them out of tasks.
		return false, fmt.Errorf("cloud-init failed; inspect its status inside the instance")
	}
	if *response.ExitCode != 0 && *response.ExitCode != 2 {
		return false, fmt.Errorf("cloud-init status command failed")
	}
	switch status {
	case "done":
		// Exit 2 can mean recoverable warnings, including missing sshd/host keys
		// on Alpine cloud images. Subsequent SSH checks must prove readiness.
		return true, nil
	case "running", "not started", "not run":
		return false, nil
	default:
		return false, fmt.Errorf("cloud-init did not reach a successful terminal state")
	}
}

func cloudInitErrors(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "recoverable_errors" {
				continue
			}
			if key == "errors" {
				switch errors := child.(type) {
				case nil:
				case []any:
					if len(errors) != 0 {
						return true
					}
				default:
					return true
				}
			}
			if cloudInitErrors(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if cloudInitErrors(child) {
				return true
			}
		}
	}
	return false
}

func sshPolicy(passwordLogin, active bool) string {
	root, password, publickey := "no", "no", "no"
	if active {
		root, publickey = "prohibit-password", "yes"
		if passwordLogin {
			root, password = "yes", "yes"
		}
	}
	return "# Managed by Particeps for root SSH access.\n" +
		"Port 22\nPermitRootLogin " + root + "\nPasswordAuthentication " + password +
		"\nPubkeyAuthentication " + publickey + "\nKbdInteractiveAuthentication no\nPermitEmptyPasswords no\nAuthenticationMethods any\n"
}

func sshPolicyScript(passwordLogin, active bool) string {
	return `umask 077
policy_tmp=$(mktemp /etc/ssh/.particeps-root.XXXXXX)
trap 'rm -f "$policy_tmp"' EXIT
cat > "$policy_tmp" <<'PARTICEPS_SSH_POLICY'
` + sshPolicy(passwordLogin, active) + `PARTICEPS_SSH_POLICY
chmod 644 "$policy_tmp"
mv -f "$policy_tmp" /etc/ssh/particeps-root.conf
trap - EXIT
`
}

func checkSSHPolicy(output string, passwordLogin, active bool) error {
	values := map[string][]string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			values[fields[0]] = append(values[fields[0]], strings.Join(fields[1:], " "))
		}
	}
	root, password, publickey := "no", "no", "no"
	if active {
		root, publickey = "without-password", "yes"
		if passwordLogin {
			root, password = "yes", "yes"
		}
	}
	if len(values["permitrootlogin"]) == 1 && values["permitrootlogin"][0] == "prohibit-password" {
		values["permitrootlogin"][0] = "without-password"
	}
	for key, want := range map[string]string{
		"port": "22", "permitrootlogin": root, "passwordauthentication": password,
		"pubkeyauthentication": publickey, "kbdinteractiveauthentication": "no",
		"permitemptypasswords": "no", "authenticationmethods": "any",
	} {
		if got := values[key]; len(got) != 1 || got[0] != want {
			return fmt.Errorf("effective SSH configuration does not match the requested login policy (%s)", key)
		}
	}
	return nil
}

const sshShellPreamble = `set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
`

const cloudInitStatusScript = `set -u
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
command -v cloud-init >/dev/null 2>&1 || exit 1
status=$(cloud-init status --format json)
code=$?
printf '{"exit_code":%s,"result":%s}\n' "$code" "$status"
`

const sshPrepareScript = sshShellPreamble + `
. /etc/os-release
case "$ID:$VERSION_ID" in
    alpine:3.21|alpine:3.21.*)
        if rc-service sshd status >/dev/null 2>&1; then
            rc-service sshd stop >/dev/null 2>&1
        fi
        if [ ! -x /usr/sbin/sshd ] || [ ! -x /etc/init.d/sshd ]; then
            apk add --no-cache openssh >/dev/null 2>&1
        fi
        ;;
    debian:13|debian:13.*)
        if systemctl is-active --quiet ssh.service; then
            systemctl stop ssh.service
        fi
        if systemctl is-active --quiet ssh.socket; then
            systemctl stop ssh.socket
        fi
        # Package post-install hooks must not open login before credentials exist.
        systemctl mask --runtime ssh.service ssh.socket >/dev/null 2>&1
        if [ ! -x /usr/sbin/sshd ]; then
            export DEBIAN_FRONTEND=noninteractive UCF_FORCE_CONFFOLD=1
            apt-get -o Acquire::Retries=2 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 update >/dev/null 2>&1
            apt-get -y -o DPkg::Lock::Timeout=30 -o Acquire::Retries=2 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 --no-install-recommends install openssh-server >/dev/null 2>&1
        fi
        ;;
    *) exit 1 ;;
esac
mkdir -p /etc/ssh /run/sshd
chmod 755 /run/sshd
ssh-keygen -A >/dev/null 2>&1
` + sshIncludeScript

const sshIncludeScript = `
# OpenSSH uses the first value it reads. Cloud images may contain only a
# PasswordAuthentication stub, so an existing Include cannot be assumed.
config_tmp=$(mktemp /etc/ssh/.particeps-sshd.XXXXXX)
trap 'rm -f "$config_tmp"' EXIT
printf 'Include /etc/ssh/particeps-root.conf\n' > "$config_tmp"
if [ -f /etc/ssh/sshd_config ]; then
    sed '\|^Include /etc/ssh/particeps-root.conf$|d' /etc/ssh/sshd_config >> "$config_tmp"
fi
chmod 600 "$config_tmp"
mv -f "$config_tmp" /etc/ssh/sshd_config
trap - EXIT
`

const sshReadPolicyScript = `/usr/sbin/sshd -t >/dev/null 2>&1
/usr/sbin/sshd -T -C user=root,host=particeps,addr=192.0.2.1
`

const sshRootCredentialScript = `
# A locked account can reject public keys on Alpine even with a valid key.
# Inspect without emitting the shadow entry or the authorized key.
awk -F: '$1 == "root" && $2 != "" && $2 !~ /^[!*]/ { ok=1 } END { exit !ok }' /etc/shadow
if [ "$1" = publickey ]; then
    test -s /root/.ssh/authorized_keys
    ssh-keygen -l -f /root/.ssh/authorized_keys >/dev/null 2>&1
fi
`

const sshActivateScript = sshShellPreamble + sshRootCredentialScript + `
. /etc/os-release
case "$ID:$VERSION_ID" in
    alpine:3.21|alpine:3.21.*)
        rc-update add sshd default >/dev/null 2>&1
        rc-service sshd restart >/dev/null 2>&1
        rc-service sshd status >/dev/null 2>&1
        ;;
    debian:13|debian:13.*)
        systemctl unmask --runtime ssh.service ssh.socket >/dev/null 2>&1
        if systemctl cat ssh.socket >/dev/null 2>&1; then
            systemctl disable --now ssh.socket >/dev/null 2>&1
        fi
        systemctl enable ssh.service >/dev/null 2>&1
        systemctl restart ssh.service
        systemctl is-active --quiet ssh.service
        ;;
    *) exit 1 ;;
esac
` + sshListenScript

const sshListenScript = `
attempt=0
while [ "$attempt" -lt 15 ]; do
    if { cat /proc/net/tcp; if [ -r /proc/net/tcp6 ]; then cat /proc/net/tcp6; fi; } |
        awk '$4 == "0A" {
            split($2, address, ":")
            if (address[2] != "0016") next
            if (length(address[1]) == 8 && substr(address[1], 7, 2) == "7F") next
            if (address[1] == "00000000000000000000000001000000") next
            if (substr(address[1], 1, 24) == "0000000000000000FFFF0000" && substr(address[1], 31, 2) == "7F") next
            ok=1
        } END { exit !ok }'; then
        break
    fi
    attempt=$((attempt + 1))
    sleep 1
done
test "$attempt" -lt 15
`
