"""Exercise deploy/install.sh against mocked host tools. Linux or Git Bash."""
from __future__ import annotations

import hashlib
import os
import sqlite3
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


ELF64 = b"\x7fELF\x02" + bytes(20) + b"new-agent"


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
        for path in (self.bin, self.etc, self.unit_dir, self.data, self.mock, self.root / "run" / "lock"):
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
case "$1" in
  is-active) test "${TEST_ACTIVE:-1}" = 1 ;;
  restart) test "${TEST_RESTART_FAIL:-0}" != 1 ;;
  stop|disable|daemon-reload|enable|show) exit 0 ;;
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

    def test_update_preserves_config_and_wal(self):
        _, calls = self.run_script("--update-only", "--non-interactive")
        self.assertIn("systemctl restart particeps-agent", calls)
        self.assertIn("releases/latest/download", calls)
        self.assertEqual((self.bin / "particeps-agent").read_bytes(), ELF64)
        self.assertIn("keep: original", self.config.read_text(encoding="utf-8"))
        backups = list((self.data / "backups").glob("upgrade-*"))
        self.assertEqual(len(backups), 1)
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
        self.assertIn("ELF64", output)
        self.assertEqual((self.bin / "particeps-agent").read_text(encoding="utf-8"), "old-agent")
        self.assertNotIn("systemctl restart", calls)

    def test_restart_failure_is_not_success(self):
        self.env["TEST_RESTART_FAIL"] = "1"
        output, _ = self.run_script("--update-only", "-y", success=False)
        self.assertIn("备份:", output)

    def test_update_only_requires_install(self):
        self.config.unlink()
        output, calls = self.run_script("--update-only", "-y", success=False)
        self.assertIn("未找到标准安装", output)
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
        self.assertIn("初始密码:", output)
        self.assertNotIn("admin-bootstrap.txt", output)
        self.assertFalse((self.data / "admin-bootstrap.txt").exists())

if __name__ == "__main__":
    unittest.main()
