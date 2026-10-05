"""Regression coverage for Git selection and non-mutating contract drift checks."""

import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("checks", Path(__file__).with_name("checks.py"))
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)
PUSH_HOOK = Path(__file__).with_name("pre-push").resolve()


class RepoTest(unittest.TestCase):
    def setUp(self):
        clean_env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        clean_env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_SYSTEM=os.devnull)
        env_patch = patch.dict(os.environ, clean_env, clear=True)
        env_patch.start()
        self.addCleanup(env_patch.stop)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.patch = patch.object(checks, "ROOT", self.root)
        self.patch.start()
        self.addCleanup(self.patch.stop)
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.name", "Hook tests")
        self.git("config", "user.email", "hooks@example.test")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "core.hooksPath", "/dev/null")
        self.write("core/a.go", "package core\n")
        self.git("add", ".")
        self.git("commit", "-qm", "base")
        self.git("update-ref", "refs/remotes/origin/main", "HEAD")

    def git(self, *args):
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_SYSTEM=os.devnull)
        return subprocess.check_output(["git", *args], cwd=self.root, stderr=subprocess.PIPE, env=env)

    def write(self, path, content):
        file = self.root / path
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(content)

    def test_commits_staged_unstaged_untracked_and_ignored(self):
        self.write("cli/a.go", "package cli\n")
        self.git("add", ".")
        self.git("commit", "-qm", "cli")
        self.write("core/a.go", "package changed\n")
        self.git("add", "core/a.go")
        self.write("mcp/new file.go", "package mcp\n")
        self.write(".gitignore", "ignored/\n")
        self.write("ignored/web-ui/new.js", "ignored")
        changed = checks.changed_paths("fast")
        self.assertEqual(checks.selected_modules(changed), {"core", "cli", "mcp"})
        self.assertIn("mcp/new file.go", changed)
        self.assertNotIn("ignored/web-ui/new.js", changed)
        self.assertEqual(checks.changed_paths("static"), {"core/a.go"})

    def test_merge_base_keeps_branch_changes_after_main_advances(self):
        self.git("checkout", "-qb", "feature")
        self.write("cli/a.go", "package cli\n")
        self.git("add", ".")
        self.git("commit", "-qm", "feature")
        self.git("checkout", "-q", "main")
        self.write("mcp/a.go", "package mcp\n")
        self.git("add", ".")
        self.git("commit", "-qm", "new main")
        self.git("update-ref", "refs/remotes/origin/main", "HEAD")
        self.git("checkout", "-q", "feature")
        self.assertEqual(checks.selected_modules(checks.changed_paths("fast")), {"cli"})

    def test_cross_module_rename_checks_both_sides(self):
        (self.root / "mcp").mkdir()
        self.git("mv", "core/a.go", "mcp/a.go")
        self.assertEqual(checks.selected_modules(checks.changed_paths("fast")), {"core", "mcp"})

    def test_opposing_staged_and_unstaged_changes_do_not_cancel(self):
        self.write("core/a.go", "package staged\n")
        self.git("add", "core/a.go")
        self.write("core/a.go", "package core\n")
        self.assertEqual(checks.selected_modules(checks.changed_paths("fast")), {"core"})
        with self.assertRaisesRegex(RuntimeError, "staged snapshot"):
            checks.static_checks(checks.changed_paths("static"))

    def test_untracked_build_input_cannot_mask_staged_type_error(self):
        self.write("core/a.go", "package core\nfunc A() { B() }\n")
        self.git("add", "core/a.go")
        self.write("core/helper.go", "package core\nfunc B() {}\n")
        with self.assertRaisesRegex(RuntimeError, "core/helper.go"):
            checks.static_checks(checks.changed_paths("static"))

    def test_gitignored_go_source_cannot_mask_staged_type_error(self):
        self.write("core/a.go", "package core\nfunc A() { B() }\n")
        self.write(".gitignore", "core/helper.go\n")
        self.git("add", "core/a.go", ".gitignore")
        self.write("core/helper.go", "package core\nfunc B() {}\n")
        with self.assertRaisesRegex(RuntimeError, "core/helper.go"):
            checks.static_checks(checks.changed_paths("static"))

    def test_shared_changes_and_missing_base_are_conservative(self):
        for path in ("Makefile", "scripts/git-hooks/checks.py", "contracts/anx-schema.yaml", "pnpm-lock.yaml",
                     "core/cmd/contract-gen/main.go", "core/cmd/route-inventory/main.go"):
            self.assertEqual(checks.selected_modules({path}), set(checks.MODULES))
        self.git("update-ref", "-d", "refs/remotes/origin/main")
        # Fallback must include every module even if a module has no tracked files yet.
        self.assertEqual(checks.selected_modules(checks.changed_paths("fast")), set(checks.MODULES))
        with self.assertRaises(subprocess.CalledProcessError):
            checks.changed_paths("fast", base="nonexistent")

    def test_docs_only_do_not_run_suites(self):
        self.assertEqual(checks.selected_modules({"AGENTS.md", "docs/architecture/example.md"}), set())

    def test_fast_commands_exclude_browsers_and_real_binary_integration(self):
        with patch.object(checks, "run") as run:
            checks.fast_tests({"core/a.go", "cli/a.go", "mcp/a.go", "web-ui/a.js"})
        commands = [call.args for call in run.call_args_list]
        self.assertEqual(commands.count(("go", "test", "-short", "./...")), 3)
        self.assertIn(("pnpm", "-C", "web-ui", "run", "test:unit"), commands)
        self.assertFalse(any("playwright" in str(c) or "integration" in str(c) for c in commands))

    def push(self, content):
        return subprocess.run([str(PUSH_HOOK), "origin", "local-test-remote"], cwd=self.root,
                              input=content, text=True, capture_output=True)

    def fake_pre_commit(self):
        self.write(".venv/bin/pre-commit", "#!/usr/bin/env python3\nimport sys\nprint(sys.stdin.read(), end='')\n")
        (self.root / ".venv/bin/pre-commit").chmod(0o755)

    def test_push_deletions_skip_checks_and_head_input_is_preserved(self):
        head = self.git("rev-parse", "HEAD").decode().strip()
        deletion = f"(delete) {'0' * 40} refs/heads/old {head}\n"
        self.assertEqual(self.push(deletion).returncode, 0)
        self.fake_pre_commit()
        update = f"refs/heads/main {head} refs/heads/main {'0' * 40}\n"
        result = self.push(update + deletion)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, update + deletion)

    def test_push_rejects_other_tree_and_accepts_annotated_tag_at_head(self):
        base = self.git("rev-parse", "HEAD").decode().strip()
        self.write("cli/second.go", "package cli\n")
        self.git("add", ".")
        self.git("commit", "-qm", "second")
        result = self.push(f"refs/heads/old {base} refs/heads/old {'0' * 40}\n")
        self.assertEqual(result.returncode, 1)
        self.assertIn("Check out refs/heads/old", result.stderr)
        self.fake_pre_commit()
        self.git("tag", "-a", "vtest", "-m", "test tag")
        tag = self.git("rev-parse", "vtest").decode().strip()
        result = self.push(f"refs/tags/vtest {tag} refs/tags/vtest {'0' * 40}\n")
        self.assertEqual(result.returncode, 0, result.stderr)

    def fake_generator(self, *argv, **kwargs):
        if "./cmd/contract-gen" in argv:
            out = Path(argv[argv.index("--out") + 1])
            for path in ("meta/commands.json", "meta/event_ref_rules.json", "meta/taxonomy.json",
                         "docs/commands.md", "go/client/client_gen.go", "ts/client.ts"):
                file = out / path
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text("generated\n")
        elif "./cmd/route-inventory" in argv:
            Path(argv[argv.index("--out") + 1]).write_text("generated\n")

    def stage_generated(self):
        for path in ("contracts/gen/meta/commands.json", "contracts/gen/meta/event_ref_rules.json",
                     "contracts/gen/meta/taxonomy.json", "contracts/gen/meta/routes.json",
                     "contracts/gen/docs/commands.md", "contracts/gen/go/client/client_gen.go",
                     "contracts/gen/ts/client.ts", "cli/internal/registry/commands.json",
                     "cli/internal/registry/event_ref_rules.json", "cli/internal/registry/taxonomy.json",
                     "cli/docs/generated/commands.md", "web-ui/src/lib/generated/event_ref_rules.json",
                     "web-ui/src/lib/generated/taxonomy.json"):
            self.write(path, "generated\n")
        self.git("add", ".")

    def test_drift_uses_index_and_does_not_overwrite_unstaged_work(self):
        self.stage_generated()
        path = "contracts/gen/ts/client.ts"
        self.write(path, "unstaged work\n")
        with patch.object(checks, "run", side_effect=self.fake_generator):
            checks.contract_drift(staged=True)
            with self.assertRaisesRegex(RuntimeError, "client.ts"):
                checks.contract_drift()
        self.assertEqual((self.root / path).read_text(), "unstaged work\n")

    def test_drift_rejects_missing_extra_and_changed_mirrors(self):
        self.stage_generated()
        self.git("rm", "-qf", "contracts/gen/ts/client.ts")
        self.write("cli/internal/registry/obsolete.json", "old\n")
        self.write("web-ui/src/lib/generated/taxonomy.json", "stale\n")
        self.git("add", ".")
        with patch.object(checks, "run", side_effect=self.fake_generator):
            with self.assertRaises(RuntimeError) as failure:
                checks.contract_drift(staged=True)
        for name in ("client.ts", "obsolete.json", "taxonomy.json"):
            self.assertIn(name, str(failure.exception))


if __name__ == "__main__":
    unittest.main()
