"""Offline release-installer contract tests (no public network or real home)."""
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "install-anx.sh"


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.mock = self.root / "mock"
        self.mock.mkdir()
        self.install = self.root / "install"
        os_name = "darwin" if platform.system() == "Darwin" else "linux"
        arch = "arm64" if platform.machine() in ("arm64", "aarch64") else "amd64"
        self.archive_name = f"anx_v0.12.11_{os_name}_{arch}.tar.gz"
        self.binary = b'#!/bin/sh\necho "synced"\n'
        with tarfile.open(self.root / self.archive_name, "w:gz") as archive:
            info = tarfile.TarInfo("anx")
            info.mode = 0o755
            info.size = len(self.binary)
            archive.addfile(info, io.BytesIO(self.binary))
        digest = hashlib.sha256((self.root / self.archive_name).read_bytes()).hexdigest()
        (self.root / "checksums.txt").write_text(f"{digest}  {self.archive_name}\n")
        curl = self.mock / "curl"
        curl.write_text('''#!/bin/sh
out=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -w) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  */releases/latest)
    case "$url" in
      https://api.github.com/*) [ "$MOCK_RATE_LIMIT" = 1 ] && exit 22; echo '{"tag_name":"v0.12.11"}' ;;
      *) echo 'https://github.com/Git-on-my-level/agent-nexus/releases/tag/v0.12.11' ;;
    esac ;;
  */download/*) cp "$MOCK_ROOT/${url##*/}" "$out" ;;
  *) exit 22 ;;
esac
''')
        curl.chmod(0o755)
        self.env = dict(os.environ, HOME=str(self.root / "home"), INSTALL_DIR=str(self.install),
                        PATH=f"{self.mock}:/usr/bin:/bin", MOCK_ROOT=str(self.root), VERSION="")

    def run_installer(self):
        return subprocess.run(["bash", str(SCRIPT)], env=self.env, capture_output=True, text=True)

    def test_receipt_matches_binary_and_skills_sync_runs(self):
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        receipt = json.loads((self.install / "anx.anx-install.json").read_text())
        self.assertEqual(receipt["managed_by"], "anx")
        self.assertEqual(receipt["version"], "v0.12.11")
        self.assertEqual(receipt["sha256"], hashlib.sha256((self.install / "anx").read_bytes()).hexdigest())
        self.assertIn("synced", result.stdout)
        self.assertFalse((self.install / "anx.anx-update.lock").exists())

    def test_rate_limit_falls_back_to_release_redirect(self):
        self.env["MOCK_RATE_LIMIT"] = "1"
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.install / "anx.anx-install.json").exists())

    def test_bad_checksum_leaves_existing_install_untouched(self):
        self.install.mkdir()
        (self.install / "anx").write_bytes(b"original")
        (self.root / "checksums.txt").write_text("0" * 64 + "  " + self.archive_name + "\n")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.install / "anx").read_bytes(), b"original")
        self.assertFalse((self.install / "anx.anx-install.json").exists())

    def test_updater_lock_prevents_installer_race(self):
        self.install.mkdir()
        (self.install / "anx").write_bytes(b"original")
        (self.install / "anx.anx-update.lock").touch()
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.install / "anx").read_bytes(), b"original")
        self.assertTrue((self.install / "anx.anx-update.lock").exists())


if __name__ == "__main__":
    unittest.main()
