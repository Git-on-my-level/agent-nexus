"""Adversarial installer tests: real probes, locks and filesystem transactions,
with only release HTTP mocked. No public network or real user home is accessed.
"""
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).resolve().parents[1] / "install-anx.sh"
PROGRAM = SCRIPT.read_text().split("<<'PY'\n", 1)[1].rsplit("\nPY", 1)[0]


def load_installer():
    namespace = {"__name__": "installer_test"}
    exec(compile(PROGRAM, str(SCRIPT), "exec"), namespace)
    return namespace


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.install = self.root / "install"
        os_name = "darwin" if platform.system() == "Darwin" else "linux"
        arch = "arm64" if platform.machine() in ("arm64", "aarch64") else "amd64"
        self.archive_name = f"anx_v0.12.11_{os_name}_{arch}.tar.gz"
        self.binary = b'''#!/bin/sh
if [ "$2" = version ]; then
 echo '{"ok":true,"result":{"cli_version":"v0.12.11"}}'
else
 echo '{"ok":true,"result":{"skills":[]}}'
fi
'''
        self.write_archive([( "anx", self.binary, tarfile.REGTYPE)])
        self.env = dict(os.environ, HOME=str(self.root / "home"), INSTALL_DIR=str(self.install),
                        MOCK_ROOT=str(self.root), VERSION="")

    def write_archive(self, entries):
        with tarfile.open(self.root / self.archive_name, "w:gz") as archive:
            for name, content, kind in entries:
                info = tarfile.TarInfo(name)
                info.mode, info.type, info.size = 0o755, kind, len(content)
                archive.addfile(info, io.BytesIO(content))
        digest = hashlib.sha256((self.root / self.archive_name).read_bytes()).hexdigest()
        (self.root / "checksums.txt").write_text(f"{digest}  {self.archive_name}\n")

    def run_installer(self):
        # Exercise the actual embedded program in a subprocess, with a fixture
        # HTTP adapter injected by the test (no production bypass/env switch).
        driver = '''import os, sys, pathlib, urllib.error
namespace={"__name__":"installer_test"}
exec(PROGRAM,namespace)
def download(url,kind,limit=128*1024*1024):
 if kind=="api":
  if os.getenv("MOCK_RATE_LIMIT")=="1": raise urllib.error.HTTPError(url,429,"rate limit",{},None)
  return b'{"tag_name":"v0.12.11"}',url
 if kind=="latest": return b"","https://github.com/Git-on-my-level/agent-nexus/releases/tag/v0.12.11"
 return (pathlib.Path(os.environ["MOCK_ROOT"])/url.rsplit("/",1)[1]).read_bytes(),url
namespace["download"]=download
try: namespace["main"]()
except Exception as error:
 print("Error:",error,file=sys.stderr);sys.exit(1)
'''
        return subprocess.run([sys.executable, "-c", "PROGRAM=" + repr(PROGRAM) + "\n" + driver],
                              env=self.env, capture_output=True, text=True, timeout=30)

    def seed_original(self):
        self.install.mkdir(exist_ok=True)
        path = self.install / "anx"
        path.write_bytes(b"original")
        path.chmod(0o755)
        receipt = {"managed_by": "anx", "version": "v0.12.10", "sha256": hashlib.sha256(b"original").hexdigest()}
        (self.install / "anx.anx-install.json").write_text(json.dumps(receipt))
        return receipt

    def test_receipt_matches_probed_binary(self):
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        receipt = json.loads((self.install / "anx.anx-install.json").read_text())
        self.assertEqual(receipt["managed_by"], "anx")
        self.assertEqual(receipt["version"], "v0.12.11")
        self.assertEqual(receipt["sha256"], hashlib.sha256((self.install / "anx").read_bytes()).hexdigest())
        self.assertIn("managed skills synchronized", result.stdout)
        # Permanent lock inode remains, but an exited process owns no lock.
        with open(self.install / "anx.anx-update.lock", "r+") as lock:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)

    def test_rate_limit_falls_back_to_release_redirect(self):
        self.env["MOCK_RATE_LIMIT"] = "1"
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_bad_checksum_preserves_original(self):
        original = self.seed_original()
        (self.root / "checksums.txt").write_text("0" * 64 + "  " + self.archive_name + "\n")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.install / "anx").read_bytes(), b"original")
        self.assertEqual(json.loads((self.install / "anx.anx-install.json").read_text()), original)

    def test_invalid_checksummed_executable_cannot_replace_healthy_install(self):
        original = self.seed_original()
        self.write_archive([("anx", b"invalid executable bytes", tarfile.REGTYPE)])
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.install / "anx").read_bytes(), b"original")
        self.assertEqual(json.loads((self.install / "anx.anx-install.json").read_text()), original)

    def test_wrong_version_executable_is_rejected(self):
        self.seed_original()
        self.write_archive([("anx", self.binary.replace(b"v0.12.11", b"v0.12.9"), tarfile.REGTYPE)])
        self.assertNotEqual(self.run_installer().returncode, 0)
        self.assertEqual((self.install / "anx").read_bytes(), b"original")

    def test_live_kernel_lock_cannot_be_stolen_by_age(self):
        self.seed_original()
        lock_path = self.install / "anx.anx-update.lock"
        with open(lock_path, "w+") as lock:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            os.utime(lock_path, (1, 1))
            inode = lock_path.stat().st_ino
            result = self.run_installer()
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual((self.install / "anx").read_bytes(), b"original")
            self.assertEqual(lock_path.stat().st_ino, inode)
        # Stale bytes/inode need no deletion once its owner releases the lock.
        self.assertEqual(self.run_installer().returncode, 0)

    def test_archive_paths_and_symlinks_are_never_extracted(self):
        for name, kind in [("../escaped", tarfile.REGTYPE), ("/absolute", tarfile.REGTYPE), ("anx", tarfile.SYMTYPE)]:
            with self.subTest(name=name):
                self.write_archive([(name, b"", kind), ("anx", self.binary, tarfile.REGTYPE)])
                self.assertNotEqual(self.run_installer().returncode, 0)
                self.assertFalse((self.root / "escaped").exists())
                self.assertFalse((self.install / "anx").exists())

    def test_aggregate_ignored_entry_decompression_is_bounded(self):
        # Highly compressed ignored content must count toward the same budget.
        module = load_installer()
        with mock.patch.dict(module, {"LIMIT": 1024}):
            data = io.BytesIO()
            with tarfile.open(fileobj=data, mode="w:gz") as archive:
                info = tarfile.TarInfo("ignored")
                info.size = 1025
                archive.addfile(info, io.BytesIO(b"x" * 1025))
            with self.assertRaisesRegex(RuntimeError, "expanded"):
                module["extract_binary"](data.getvalue())

    def test_entry_count_is_bounded(self):
        module = load_installer()
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode="w:gz") as archive:
            for i in range(4):
                archive.addfile(tarfile.TarInfo(f"entry{i}"), io.BytesIO())
        with mock.patch.dict(module, {"ENTRY_LIMIT": 3}):
            with self.assertRaisesRegex(RuntimeError, "Too many"):
                module["extract_binary"](data.getvalue())

    def test_hidden_metadata_counts_toward_entry_limit(self):
        module = load_installer()
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode="w:gz") as archive:
            for _ in range(4):
                info = tarfile.TarInfo("metadata")
                info.type, info.size = tarfile.GNUTYPE_LONGNAME, 4
                archive.addfile(info, io.BytesIO(b"anx\x00"))
            info = tarfile.TarInfo("anx")
            info.size = len(self.binary)
            archive.addfile(info, io.BytesIO(self.binary))
        with mock.patch.dict(module, {"ENTRY_LIMIT": 3}):
            with self.assertRaisesRegex(RuntimeError, "Too many"):
                module["extract_binary"](data.getvalue())

    def test_post_replace_probe_failure_restores_binary_and_receipt(self):
        original = self.seed_original()
        module = load_installer()
        path = self.install / "anx"
        real_probe = module["probe"]
        def probe(candidate, version):
            if candidate == path:
                raise RuntimeError("post-replace failure")
            return real_probe(candidate, version)
        with mock.patch.dict(module, {"probe": probe}):
            with self.assertRaisesRegex(RuntimeError, "post-replace"):
                module["install"](path, self.binary, "v0.12.11")
        self.assertEqual(path.read_bytes(), b"original")
        self.assertEqual(json.loads(module["receipt_path"](path).read_text()), original)

    def test_crash_between_binary_and_receipt_has_recoverable_journal(self):
        original = self.seed_original()
        module = load_installer()
        path = self.install / "anx"
        real_write = module["atomic_json"]
        def crash_before_receipt(destination, value):
            if destination == module["receipt_path"](path):
                raise SystemExit("simulated crash")
            real_write(destination, value)
        with mock.patch.dict(module, {"atomic_json": crash_before_receipt}):
            with self.assertRaises(SystemExit):
                module["install"](path, self.binary, "v0.12.11")
        journal = json.loads(module["journal_path"](path).read_text())
        self.assertEqual(journal["phase"], "prepared")
        self.assertTrue(Path(journal["backup_path"]).exists())
        self.assertEqual(path.read_bytes(), self.binary)
        with module["install_lock"](path):
            module["recover"](path)
        self.assertEqual(path.read_bytes(), b"original")
        self.assertEqual(json.loads(module["receipt_path"](path).read_text()), original)

    def test_drifted_skills_fail_without_overwriting_the_copy(self):
        module = load_installer()
        path = self.install / "anx"
        drifted = self.binary.replace(b'"skills":[]', b'"skills":[{"state":"drifted"}]')
        with self.assertRaisesRegex(RuntimeError, "skills need attention"):
            module["install"](path, drifted, "v0.12.11")
        self.assertEqual(path.read_bytes(), drifted)
        self.assertEqual(json.loads(module["receipt_path"](path).read_text())["version"], "v0.12.11")

    def test_redirects_reject_hosts_downgrades_and_repository_changes(self):
        module = load_installer()
        entry = module["RELEASES"] + "/download/v0.12.11/anx.tar.gz"
        for target in ["https://evil.example/anx", "http://release-assets.githubusercontent.com/anx", "https://github.com/other/repo/releases/download/v0.12.11/anx.tar.gz"]:
            with self.subTest(target=target), self.assertRaises(RuntimeError):
                module["validate_redirect"](entry, target, "asset")
        module["validate_redirect"](entry, "https://release-assets.githubusercontent.com/asset", "asset")
        with self.assertRaises(RuntimeError):
            module["validate_redirect"](module["API"], "https://api.github.com/repos/other/repo/releases/latest", "api")


if __name__ == "__main__":
    unittest.main()
