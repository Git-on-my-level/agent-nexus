"""Exercise release preflight in disposable Git repos; never publish anything."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1]


class ReleasePreflightTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.calls = self.root / "calls"
        self.env = dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}",
                        CALLS=str(self.calls), GIT_CONFIG_NOSYSTEM="1",
                        GIT_CONFIG_GLOBAL=os.devnull)
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "release@example.invalid")
        scripts = self.repo / "scripts"
        (scripts / "git-hooks").mkdir(parents=True)
        shutil.copy2(SOURCE / "release-patch.sh", scripts)
        for hook in ("pre-commit", "pre-push"):
            shutil.copy2(SOURCE / "git-hooks" / hook, scripts / "git-hooks")
        self.executable(scripts / "set-version.sh", 'echo bump >> "$CALLS"\nexit 71')
        (self.repo / ".gitignore").write_text(".venv/\n")
        self.git("add", ".")
        self.git("commit", "-m", "fixture")
        self.git("tag", "v0.0.1")
        # A local remote ref suffices for the dry-run checks. Fetch is real, but
        # points only at this disposable repository.
        self.git("remote", "add", "origin", str(self.repo))
        self.git("fetch", "origin", "main")
        self.git("config", "core.hooksPath", "scripts/git-hooks")
        self.executable(self.bin / "gh", 'echo gh >> "$CALLS"')
        self.executable(self.bin / "make", 'echo make >> "$CALLS"\nif [ "${DIRTY_CHECK:-}" = 1 ]; then touch check-output.log; fi')

    def executable(self, path, body):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("#!/usr/bin/env bash\nset -eu\n" + body + "\n")
        path.chmod(0o755)

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo,
                                       env=self.env, stderr=subprocess.DEVNULL)

    def run_release(self, *args):
        return subprocess.run(["bash", "scripts/release-patch.sh", *args],
                              cwd=self.repo, env=self.env, text=True,
                              stdout=subprocess.PIPE, stderr=subprocess.STDOUT)

    def tooling(self, body="exit 0"):
        self.executable(self.repo / ".venv/bin/pre-commit", body)

    def assert_early_failure(self, message):
        result = self.run_release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(message, result.stdout)
        self.assertFalse(self.calls.exists(), result.stdout)
        self.assertEqual(self.git("diff", "--cached", "--name-only"), b"")

    def test_missing_hook_tooling_fails_before_checks(self):
        self.assert_early_failure("release hook tooling is missing")

    def test_broken_hook_tooling_fails_before_checks(self):
        self.tooling("exit 1")
        self.assert_early_failure("release hook tooling is broken")

    def test_untracked_log_fails_before_tooling_or_network(self):
        (self.repo / "release.log").write_text("log")
        self.assert_early_failure("working tree has untracked files")

    def test_tracked_edit_fails_before_tooling_or_network(self):
        (self.repo / ".gitignore").write_text("changed")
        self.assert_early_failure("working tree has unstaged changes")

    def test_check_output_fails_before_version_bump(self):
        self.tooling()
        self.env["DIRTY_CHECK"] = "1"
        result = self.run_release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("check-output.log", result.stdout)
        self.assertNotIn("bump", self.calls.read_text())
        self.assertEqual(self.git("diff", "--cached", "--name-only"), b"")

    def test_clean_checkout_reaches_version_step(self):
        self.tooling()
        result = self.run_release("--skip-checks")
        self.assertEqual(result.returncode, 71, result.stdout)
        self.assertIn("bump", self.calls.read_text())

    def test_dry_run_does_not_require_commit_tooling(self):
        result = self.run_release("--dry-run")
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("next version: v0.0.2", result.stdout)
        self.assertFalse(self.calls.exists())

    def test_linked_worktree_checks_its_own_tooling(self):
        self.tooling()
        linked = self.root / "linked"
        self.git("worktree", "add", "-b", "release-test", str(linked))
        self.repo = linked
        self.assert_early_failure("release hook tooling is missing")


if __name__ == "__main__":
    unittest.main()
