"""Exercise deploy/install.sh against mocked host tools. Linux or Git Bash."""
from __future__ import annotations

import hashlib
import os
import select
import signal
import sqlite3
import stat
import subprocess
import tempfile
import time
import unittest
from pathlib import Path

if os.name == "posix":
    import pty


ELF64 = b"\x7fELF\x02" + bytes(20) + b"new-agent"


class MenuSession:
    """Drive a real controlling terminal, including its foreground SIGINT."""

    def __init__(self, script, root, env):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(root)
            os.execvpe("bash", ["bash", str(script)], env)
        self.pending = b""
        self.transcript = b""
        self.returncode = None

    def send(self, value):
        os.write(self.fd, value.encode("utf-8"))

    def read(self, timeout=0.2):
        if not select.select([self.fd], [], [], timeout)[0]:
            return
        try:
            chunk = os.read(self.fd, 65536)
        except OSError:
            chunk = b""
        self.pending += chunk
        self.transcript += chunk

    def expect(self, value, timeout=8):
        needle = value.encode("utf-8")
        deadline = time.monotonic() + timeout
        while needle not in self.pending:
            if time.monotonic() >= deadline:
                raise AssertionError(f"Missing {value!r}:\n{self.transcript.decode('utf-8', 'replace')}")
            self.read()
        end = self.pending.index(needle) + len(needle)
        self.pending = self.pending[end:]

    def wait(self, timeout=8):
        deadline = time.monotonic() + timeout
        while self.returncode is None:
            pid, status = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                self.returncode = os.waitstatus_to_exitcode(status)
                break
            if time.monotonic() >= deadline:
                raise AssertionError("Menu did not exit:\n" + self.transcript.decode("utf-8", "replace"))
            self.read()
        return self.returncode

    def close(self):
        if self.returncode is None:
            try:
                os.killpg(self.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            _, status = os.waitpid(self.pid, 0)
            self.returncode = os.waitstatus_to_exitcode(status)
        os.close(self.fd)


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="particeps-install-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "opt" / "particeps"
        self.etc = self.root / "etc" / "particeps"
        self.unit_dir = self.root / "etc" / "systemd" / "system"
        self.data = self.root / "var" / "lib" / "particeps"
        self.mock = self.root / "bin"
        self.calls = self.root / "calls"
        self.tmp = self.root / "tmp"
        for path in (self.bin, self.etc, self.unit_dir, self.data, self.mock, self.tmp, self.root / "run" / "lock"):
            path.mkdir(parents=True)
        self.db = self.data / "state.db"
        with sqlite3.connect(self.db) as con:
            con.execute("PRAGMA journal_mode=WAL")
            con.execute("CREATE TABLE preserved (id TEXT)")
            con.execute("INSERT INTO preserved VALUES ('guest-and-port')")
            con.commit()
        (self.root / "incus-instances").write_text("particeps guest1 RUNNING\n", encoding="utf-8")
        self.config = self.etc / "config.yaml"
        self.config.write_text(
            f"listen: 127.0.0.1:8792\ndata_dir: {self.data.as_posix()}\nkeep: original\n",
            encoding="utf-8",
        )
        (self.bin / "particeps-agent").write_text("old-agent", encoding="utf-8")
        (self.unit_dir / "particeps-agent.service").write_text(
            f"ExecStart={self.bin.as_posix()}/particeps-agent --config {self.config.as_posix()}\n",
            encoding="utf-8",
        )
        self.new_agent = self.root / "new-agent"
        self.new_agent.write_bytes(ELF64)
        self.sums = self.root / "SHA256SUMS"
        digest = hashlib.sha256(ELF64).hexdigest()
        self.sums.write_text(f"{digest}  particeps-agent-linux-amd64\n", encoding="utf-8")
        source = Path(__file__).resolve().parents[2] / "deploy" / "install.sh"
        self.script = self.root / "install.sh"
        self.script.write_text(source.read_text(encoding="utf-8"), encoding="utf-8")
        self.env = os.environ.copy()
        self.env["PATH"] = str(self.mock) + os.pathsep + self.env.get("PATH", "")
        self.env["PARTICEPS_TEST_ROOT"] = str(self.root)
        self.env["TMPDIR"] = str(self.tmp)
        self.env["PARTICEPS_GITHUB_REPO"] = "TXL-325/particeps"
        self.env.pop("PARTICEPS_RELEASE_TAG", None)
        self.env["TEST_ACTIVE"] = "1"
        self.env["TEST_DOWNLOAD_FAIL"] = "0"
        self.env["TEST_RESTART_FAIL"] = "0"
        self.env["TEST_BAD_ELF"] = "0"
        self.tool("id", "echo 0")
        self.tool("sleep", "exit 0")
        self.tool("uname", "echo x86_64")
        self.tool("flock", "exit 0")
        self.tool(
            "systemctl",
            r"""
printf 'systemctl %s\n' "$*" >> "$TEST_CALLS"
state="$TEST_ROOT/service-active"
case "$1" in
  is-active)
    active="${TEST_ACTIVE:-1}"
    if test -f "$state"; then active=$(cat "$state"); fi
    test "$active" = 1 ;;
  restart)
    test "${TEST_RESTART_FAIL:-0}" != 1
    echo "${TEST_ACTIVE_AFTER_START:-1}" > "$state" ;;
  enable) echo "${TEST_ACTIVE_AFTER_START:-1}" > "$state" ;;
  stop)
    test "${TEST_STOP_FAIL:-0}" != 1
    echo 0 > "$state" ;;
  disable|daemon-reload|show) exit 0 ;;
  *) exit 0 ;;
esac
""",
        )
        self.tool(
            "curl",
            r"""
printf 'curl %s\n' "$*" >> "$TEST_CALLS"
test "${TEST_DOWNLOAD_FAIL:-0}" != 1 || exit 22
test "$1" = -fsSL && test "$2" = -o || exit 92
dest="$3"
src="$4"
base=$(basename "$src")
root="$TEST_ROOT"
if test "${TEST_BAD_ELF:-0}" = 1 && test "$base" = particeps-agent-linux-amd64; then
  printf '<html>error</html>' > "$dest"
  exit 0
fi
if test "$base" = particeps-agent-linux-amd64; then
  cp "$root/new-agent" "$dest"
elif test "$base" = SHA256SUMS; then
  cp "$root/SHA256SUMS" "$dest"
else
  exit 93
fi
""",
        )
        self.tool(
            "incus",
            r"""
printf 'incus %s\n' "$*" >> "$TEST_CALLS"
instances="$TEST_ROOT/incus-instances"
touch "$instances"
if test "$1" = project && test "$2" = list; then
  echo default
  echo particeps
  exit 0
fi
if test "$1" = --project; then
  proj="$2"
  cmd="$3"
  if test "$cmd" = list; then
    awk -v p="$proj" '$1==p { print $2 }' "$instances"
    exit 0
  fi
  if test "$cmd" = stop; then
    exit 0
  fi
  if test "$cmd" = delete; then
    name=""
    for arg in "$@"; do
      case "$arg" in
        --project|--force|delete|stop|list) ;;
        *)
          if test "$arg" != "$proj"; then
            name="$arg"
          fi
          ;;
      esac
    done
    if test -n "$name"; then
      awk -v p="$proj" -v n="$name" '$1!=p || $2!=n { print }' "$instances" > "$instances.tmp"
      mv "$instances.tmp" "$instances"
    fi
    exit 0
  fi
  exit 0
fi
exit 0
""",
        )
        self.tool(
            "ip",
            r"""
printf 'ip %s\n' "$*" >> "$TEST_CALLS"
if test "$1" = -4; then
  echo '2: ens33    inet 192.168.143.145/24 brd 192.168.143.255 scope global ens33'
  echo '3: docker0    inet 172.17.0.1/16 brd 172.17.255.255 scope global docker0'
  echo '4: particepsbr0    inet 10.80.0.1/24 brd 10.80.0.255 scope global particepsbr0'
  exit 0
fi
exit 0
""",
        )
        self.tool("apt-get", r"""printf 'apt-get %s\n' "$*" >> "$TEST_CALLS"; exit 0""")
        self.env["TEST_ROOT"] = str(self.root)
        self.env["TEST_CALLS"] = str(self.calls)

    def tool(self, name, body):
        path = self.mock / name
        path.write_text("#!/bin/bash\nset -e\n" + body + "\n", encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IEXEC)

    def run_script(self, *args, success=True, input_text=None):
        result = subprocess.run(
            ["bash", str(self.script), *args],
            cwd=self.root,
            env=self.env,
            text=True,
            capture_output=True,
            input=input_text,
            timeout=30,
        )
        output = result.stdout + result.stderr
        if success:
            self.assertEqual(result.returncode, 0, output)
        else:
            self.assertNotEqual(result.returncode, 0, output)
        calls = self.calls.read_text(encoding="utf-8") if self.calls.exists() else ""
        return output, calls

    def menu(self):
        session = MenuSession(self.script, self.root, self.env)
        self.addCleanup(session.close)
        session.expect("0) 退出")
        session.expect("请选择 [0-5]：")
        return session

    def test_update_preserves_config_and_wal(self):
        _, calls = self.run_script("--update-only", "--non-interactive")
        self.assertIn("systemctl restart particeps-agent", calls)
        self.assertIn("releases/latest/download", calls)
        self.assertEqual((self.bin / "particeps-agent").read_bytes(), ELF64)
        self.assertIn("keep: original", self.config.read_text(encoding="utf-8"))
        backups = list((self.data / "backups").glob("upgrade-*"))
        self.assertEqual(len(backups), 1)
        if os.name == "posix":
            self.assertEqual(backups[0].stat().st_mode & 0o777, 0o700)
        self.assertEqual((backups[0] / "particeps-agent").read_text(encoding="utf-8"), "old-agent")
        with sqlite3.connect(backups[0] / "state.db") as copied:
            self.assertEqual(copied.execute("SELECT id FROM preserved").fetchone()[0], "guest-and-port")

    def test_stopped_agent_stays_stopped(self):
        self.env["TEST_ACTIVE"] = "0"
        _, calls = self.run_script("--update-only", "-y")
        self.assertNotIn("systemctl restart", calls)

    def test_download_failure_keeps_original(self):
        self.env["TEST_DOWNLOAD_FAIL"] = "1"
        _, calls = self.run_script("--update-only", "-y", success=False)
        self.assertEqual((self.bin / "particeps-agent").read_text(encoding="utf-8"), "old-agent")
        self.assertNotIn("systemctl restart", calls)

    def test_bad_elf_keeps_original(self):
        html = b"<html>error</html>"
        self.env["TEST_BAD_ELF"] = "1"
        self.sums.write_text(f"{hashlib.sha256(html).hexdigest()}  particeps-agent-linux-amd64\n", encoding="utf-8")
        output, calls = self.run_script("--update-only", "-y", success=False)
        self.assertIn("安装文件格式不正确", output)
        self.assertEqual((self.bin / "particeps-agent").read_text(encoding="utf-8"), "old-agent")
        self.assertNotIn("systemctl restart", calls)

    def test_restart_failure_is_not_success(self):
        self.env["TEST_RESTART_FAIL"] = "1"
        output, _ = self.run_script("--update-only", "-y", success=False)
        self.assertIn("备份：", output)

    def test_start_command_success_requires_running_service(self):
        self.env["TEST_ACTIVE_AFTER_START"] = "0"
        output, _ = self.run_script("--update-only", "-y", success=False)
        self.assertIn("Agent 启动后已停止", output)
        self.assertNotIn("更新完成。", output)
        self.assertEqual(list(self.tmp.iterdir()), [])

    def test_uninstall_does_not_remove_binary_when_stop_fails(self):
        self.env["TEST_STOP_FAIL"] = "1"
        output, _ = self.run_script("--uninstall", "--keep-instances", "-y", success=False)
        self.assertIn("无法停止 Agent", output)
        self.assertTrue((self.bin / "particeps-agent").exists())
        self.assertTrue(self.db.exists())

    def test_uninstall_stops_loaded_service_when_unit_file_is_missing(self):
        (self.unit_dir / "particeps-agent.service").unlink()
        _, calls = self.run_script("--uninstall", "--keep-instances", "-y")
        self.assertIn("systemctl stop particeps-agent", calls)
        self.assertEqual((self.root / "service-active").read_text().strip(), "0")
        self.assertFalse((self.bin / "particeps-agent").exists())
        self.assertTrue(self.config.exists())

    def test_update_only_requires_install(self):
        self.config.unlink()
        output, calls = self.run_script("--update-only", "-y", success=False)
        self.assertIn("不完整的安装", output)
        self.assertNotIn("curl", calls)

    def test_status(self):
        output, _ = self.run_script("--status")
        self.assertIn("AGENT_SERVICE=", output)
        self.assertIn("LISTEN=127.0.0.1:8792", output)

    def test_rollback_restores_binary_and_db(self):
        self.run_script("--update-only", "-y")
        (self.bin / "particeps-agent").write_bytes(b"broken")
        self.config.write_text("listen: 0.0.0.0:1\n", encoding="utf-8")
        self.run_script("--rollback", "-y")
        self.assertEqual((self.bin / "particeps-agent").read_text(encoding="utf-8"), "old-agent")
        self.assertIn("keep: original", self.config.read_text(encoding="utf-8"))
        with sqlite3.connect(self.db) as con:
            self.assertEqual(con.execute("SELECT id FROM preserved").fetchone()[0], "guest-and-port")

    def test_keep_instances_skips_incus_delete(self):
        _, calls = self.run_script("--uninstall", "--keep-instances", "-y")
        self.assertNotIn("incus --project particeps delete", calls)
        self.assertNotIn("incus --project particeps stop", calls)
        self.assertNotIn("apt-get purge", calls)
        self.assertTrue(self.config.exists())
        self.assertTrue(self.db.exists())
        self.assertFalse((self.bin / "particeps-agent").exists())

    def test_full_uninstall_requires_purge(self):
        output, calls = self.run_script("--uninstall", "-y", success=False)
        self.assertIn("PURGE", output)
        self.assertTrue((self.bin / "particeps-agent").exists())
        self.assertNotIn("incus --project particeps delete", calls)

    def test_full_uninstall_with_purge(self):
        _, calls = self.run_script("--uninstall", "-y", "--confirm", "PURGE")
        self.assertIn("incus --project particeps stop --force guest1", calls)
        self.assertIn("incus --project particeps delete --force guest1", calls)
        self.assertNotIn("apt-get purge", calls)
        self.assertFalse(self.config.exists())
        self.assertFalse(self.data.exists())
        self.assertFalse(self.bin.exists())
        leftover = (self.root / "incus-instances").read_text(encoding="utf-8")
        self.assertNotIn("guest1", leftover)

    def test_fresh_install_shows_panel_once(self):
        (self.bin / "particeps-agent").unlink()
        self.config.unlink()
        (self.unit_dir / "particeps-agent.service").unlink()
        output, _ = self.run_script("-y")
        config_text = self.config.read_text(encoding="utf-8")
        self.assertIn("listen: 0.0.0.0:8792", config_text)
        self.assertIn("session_cookie_secure: false", config_text)
        self.assertIn("安装完成", output)
        self.assertIn("http://192.168.143.145:8792", output)
        self.assertNotIn("http://172.17.0.1:8792", output)
        self.assertNotIn("http://10.80.0.1:8792", output)
        self.assertIn("初始管理员密码：", output)
        self.assertNotIn("admin-bootstrap.txt", output)
        self.assertFalse((self.data / "admin-bootstrap.txt").exists())
        password = output.split("初始管理员密码：", 1)[1].splitlines()[0]
        logs = list((self.root / "var" / "log" / "particeps-install").iterdir())
        self.assertTrue(logs)
        for logfile in logs:
            self.assertNotIn(password, logfile.read_text(encoding="utf-8"))
            if os.name == "posix":
                self.assertEqual(logfile.stat().st_mode & 0o777, 0o600)

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_menu_ctrl_c_exits_script(self):
        session = self.menu()
        session.send("\x03")
        self.assertEqual(session.wait(), 130)
        self.assertNotIn("已取消当前输入".encode(), session.transcript)

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_menu_invalid_input_and_repeated_status(self):
        session = self.menu()
        session.send("invalid\n")
        session.expect("请输入 0 到 5。")
        for _ in range(2):
            session.expect("0) 退出")
            session.send("2\n")
            session.expect("particeps 状态")
            session.expect("回车返回菜单")
            session.read()
            self.assertNotIn("请选择 [0-5]：".encode(), session.pending)
            session.send("\n")
        session.expect("0) 退出")
        session.send("0\n")
        self.assertEqual(session.wait(), 0)
        self.assertEqual(list(self.tmp.iterdir()), [])

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_menu_failure_waits_allows_log_and_safe_retry(self):
        original_curl = (self.mock / "curl").read_text(encoding="utf-8")
        self.tool("curl", "echo 'injected download failure' >&2\nexit 22")
        (self.mock / "flock").unlink()  # Real flock verifies release between attempts.
        session = self.menu()
        session.send("1\n")
        session.expect("回车开始更新")
        session.send("e\n")
        session.expect("版本 [")
        session.send("bad tag\n")
        session.expect("版本只能包含")
        session.expect("版本 [")
        session.send("v0.2.0\n")
        session.expect("回车开始更新")
        session.send("\n")
        session.expect("下载 Agent 失败")
        session.expect("回车返回菜单")
        session.read()
        self.assertNotIn("请选择 [0-5]：".encode(), session.pending)
        self.assertEqual((self.bin / "particeps-agent").read_text(), "old-agent")
        self.assertNotIn("systemctl restart", self.calls.read_text())
        session.send("l\n")
        session.expect("injected download failure")
        session.expect("回车返回结果页")
        session.send("\n")
        session.expect("回车返回菜单")
        (self.mock / "curl").write_text(original_curl, encoding="utf-8")
        session.send("r\n")
        session.expect("更新完成。")
        session.expect("回车返回菜单")
        self.assertEqual((self.bin / "particeps-agent").read_bytes(), ELF64)
        self.assertIn("releases/download/v0.2.0/", self.calls.read_text())
        session.send("\n")
        session.expect("0) 退出")
        session.send("0\n")
        self.assertEqual(session.wait(), 0)
        self.assertEqual(list(self.tmp.iterdir()), [])

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_menu_unexpected_command_failure_stops_downstream_steps(self):
        self.tool("cp", r'''
case "$*" in
  *upgrade-*/config.yaml) echo 'injected backup failure' >&2; exit 17 ;;
esac
exec /bin/cp "$@"
''')
        session = self.menu()
        session.send("1\n")
        session.expect("回车开始更新")
        session.send("\n")
        session.expect("失败")
        session.expect("回车返回菜单")
        self.assertEqual((self.bin / "particeps-agent").read_text(), "old-agent")
        self.assertNotIn("systemctl restart", self.calls.read_text())
        session.send("\n")
        session.expect("0) 退出")
        session.send("0\n")
        self.assertEqual(session.wait(), 0)

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_menu_interrupt_cancels_child_cleans_staging_and_returns(self):
        self.tool("curl", r'''
echo $$ > "$TEST_ROOT/download-pid"
echo 'mock download waiting'
exec /bin/sleep 30
''')
        session = self.menu()
        session.send("1\n")
        session.expect("回车开始更新")
        session.send("\n")
        session.expect("下载 Agent…")
        # The progress message precedes curl; wait until the mock has written its PID.
        session.expect("mock download waiting")
        download_pid = int((self.root / "download-pid").read_text())
        session.send("\x03")
        session.expect("更新已取消。")
        session.expect("0) 退出")
        with self.assertRaises(ProcessLookupError):
            os.kill(download_pid, 0)
        self.assertEqual((self.bin / "particeps-agent").read_text(), "old-agent")
        session.send("2\n")
        session.expect("particeps 状态")
        session.expect("回车返回菜单")
        session.send("\n")
        session.expect("0) 退出")
        session.expect("请选择 [0-5]：")
        session.send("\x03")
        self.assertEqual(session.wait(), 130)
        self.assertEqual(list(self.tmp.iterdir()), [])

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_menu_uninstall_options_do_not_leak_and_decline_is_cancel(self):
        session = self.menu()
        session.send("5\n")
        session.expect("回车开始卸载")
        session.send("\n")
        session.expect("Agent 已卸载。")
        session.expect("回车返回菜单")
        session.send("\n")
        session.expect("0) 退出")
        session.send("4\n")
        session.expect("输入 PURGE")
        session.send("no\n")
        session.expect("0) 退出")
        session.send("0\n")
        self.assertEqual(session.wait(), 0)
        self.assertTrue(self.config.exists())
        self.assertTrue(self.db.exists())
        self.assertNotIn("incus --project particeps delete", self.calls.read_text())

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_menu_interrupting_log_view_returns_to_menu(self):
        self.tool("cat", r'''
case "${2:-}" in
  "$TEST_ROOT"/var/log/particeps-install/*)
    echo 'mock log waiting'
    exec /bin/sleep 30 ;;
esac
exec /bin/cat "$@"
''')
        session = self.menu()
        session.send("2\n")
        session.expect("回车返回菜单")
        session.send("l\n")
        session.expect("mock log waiting")
        session.send("\x03")
        self.assertEqual(session.wait(), 130)

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_menu_does_not_offer_retry_after_replacement(self):
        self.env["TEST_RESTART_FAIL"] = "1"
        session = self.menu()
        session.send("1\n")
        session.expect("回车开始更新")
        session.send("\n")
        session.expect("失败")
        session.expect("回车返回菜单")
        session.send("r\n")
        session.expect("该选项当前不可用，请重新选择。")
        session.expect("回车返回菜单")
        self.assertEqual(self.calls.read_text().count("systemctl restart particeps-agent"), 1)
        self.assertEqual((self.bin / "particeps-agent").read_bytes(), ELF64)
        session.send("\n")
        session.expect("0) 退出")
        session.send("0\n")
        self.assertEqual(session.wait(), 0)

    @unittest.skipUnless(os.name == "posix", "requires a POSIX controlling terminal")
    def test_terminal_eof_ends_menu_and_result_page(self):
        for at_result in (False, True):
            with self.subTest(at_result=at_result):
                session = self.menu()
                if at_result:
                    session.send("2\n")
                    session.expect("回车返回菜单")
                    session.expect("L 查看日志：")
                session.send("\x04")
                self.assertEqual(session.wait(), 0)
                self.assertEqual(list(self.tmp.iterdir()), [])

if __name__ == "__main__":
    unittest.main()
