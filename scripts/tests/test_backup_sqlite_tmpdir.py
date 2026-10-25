import os
from pathlib import Path
import subprocess
import tempfile
import unittest


REPO_ROOT = Path(__file__).resolve().parents[2]


class BackupSQLiteTempDirectoryTest(unittest.TestCase):
    def test_backup_sqlite_inherits_workspace_temp_directory(self):
        for override in (False, True):
            with self.subTest(override=override), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                instance = root / "instance"
                workspace = instance / "workspace"
                workspace.mkdir(parents=True)
                (workspace / "state.sqlite").touch()
                (instance / "metadata").mkdir()
                (instance / "config").mkdir()
                binaries = root / "bin"
                binaries.mkdir()
                capture = root / "sqlite-temp-path"
                sqlite = binaries / "sqlite3"
                sqlite.write_text(
                    '#!/bin/sh\n'
                    'printf "%s" "${SQLITE_TMPDIR:-}" > "$TEMP_CAPTURE"\n'
                    'exit 42\n'
                )
                sqlite.chmod(0o755)
                env = dict(os.environ)
                env.pop("SQLITE_TMPDIR", None)
                env["PATH"] = str(binaries) + os.pathsep + env["PATH"]
                env["TEMP_CAPTURE"] = str(capture)
                expected = workspace / "tmp"
                if override:
                    expected = root / "explicit-sqlite-temp"
                    expected.mkdir(mode=0o700)
                    env["SQLITE_TMPDIR"] = str(expected)
                result = subprocess.run(
                    [str(REPO_ROOT / "scripts/hosted/backup-workspace.sh"),
                     "--instance-root", str(instance), "--output-dir", str(root / "backup")],
                    env=env, capture_output=True, text=True, check=False,
                )
                self.assertEqual(result.returncode, 42, result.stderr)
                self.assertEqual(capture.read_text(), str(expected))
                self.assertEqual(expected.stat().st_mode & 0o777, 0o700)


if __name__ == "__main__":
    unittest.main()
