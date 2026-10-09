"""Regression coverage for Git selection and non-mutating contract drift checks."""

import importlib.util
import io
import json
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

    def root_package_graph(self, module):
        """Single package at the module root, matching this fixture's layout."""
        path = module + "/root"
        return {".": path}, {path: set()}, {path}

    def fast_commands(self, changed, base=None):
        with patch.object(checks, "run") as run, \
                patch.object(checks, "go_package_graph", self.root_package_graph):
            checks.fast_tests(changed, base)
        return [call.args for call in run.call_args_list]

    def test_fast_commands_exclude_browsers_and_real_binary_integration(self):
        commands = self.fast_commands({"core/a.go", "cli/a.go", "mcp/a.go", "web-ui/a.js"})
        self.assertEqual(commands.count(("go", "test", "-short", "./.")), 3)
        self.assertIn(("pnpm", "-C", "web-ui", "run", "test:unit"), commands)
        self.assertFalse(any("playwright" in str(c) or "integration" in str(c) for c in commands))

    def test_shared_and_unattributable_changes_still_run_whole_modules(self):
        for path in ("core/go.mod", "core/scripts/dev", "Makefile"):
            self.assertIn(("go", "test", "-short", "./..."), self.fast_commands({path}),
                          f"{path} must widen to the whole module")
        # Selected for a reason outside the module (VERSION feeds cli's build info).
        self.assertIn(("go", "test", "-short", "./..."), self.fast_commands({"VERSION"}))

    def test_module_docs_run_no_suite(self):
        self.write_go_module()
        with patch.object(checks, "run") as run:
            checks.fast_tests({"core/AGENTS.md", "core/docs/runbook.md"})
        self.assertEqual(run.call_args_list, [])

    def test_ui_runs_related_tests_only_for_attributable_changes(self):
        related = ("pnpm", "-C", "web-ui", "exec", "vitest", "run", "--changed", "abc123",
                   "--passWithNoTests")
        whole = ("pnpm", "-C", "web-ui", "run", "test:unit")
        with patch.object(checks, "ui_source_scanning_tests", lambda: ["tests/unit/guard.test.js"]):
            commands = self.fast_commands({"web-ui/src/lib/a.js"}, "abc123")
        self.assertIn(related, commands)
        # No module graph relates a disk-scanning guard to a change; run it anyway.
        self.assertIn(("pnpm", "-C", "web-ui", "exec", "vitest", "run",
                       "tests/unit/guard.test.js"), commands)
        # No merge base, shared UI roots and repo-wide fan-out keep the full suite.
        self.assertIn(whole, self.fast_commands({"web-ui/src/lib/a.js"}))
        self.assertNotIn(related, self.fast_commands({"web-ui/src/lib/a.js"}))
        self.assertIn(whole, self.fast_commands({"web-ui/vitest.config.js"}, "abc123"))
        self.assertIn(whole, self.fast_commands({"web-ui/tests/mocks/app-stores.js"}, "abc123"))
        self.assertIn(whole, self.fast_commands({"pnpm-lock.yaml"}, "abc123"))

    def write_go_module(self):
        # Like the real modules, the module root holds no package of its own.
        self.git("rm", "-q", "core/a.go")
        self.write("core/go.mod", "module fake\n\ngo 1.21\n")
        self.write("core/internal/leaf/leaf.go", "package leaf\n\nfunc Leaf() int { return 1 }\n")
        self.write("core/internal/leaf/leaf_test.go",
                   "package leaf\n\nimport \"testing\"\n\nfunc TestLeaf(t *testing.T) { Leaf() }\n")
        self.write("core/internal/mid/mid.go",
                   "package mid\n\nimport \"fake/internal/leaf\"\n\nfunc Mid() int "
                   "{ return leaf.Leaf() }\n")
        self.write("core/internal/helper/helper.go", "package helper\n\nfunc Help() {}\n")
        # Only a test consumes mid and helper: test imports must count as edges.
        self.write("core/internal/top/top_test.go",
                   "package top_test\n\nimport (\n\t\"testing\"\n\n\t\"fake/internal/helper\"\n"
                   "\t\"fake/internal/mid\"\n)\n\nfunc TestTop(t *testing.T) "
                   "{ helper.Help(); mid.Mid() }\n")
        self.write("core/internal/unrelated/unrelated.go", "package unrelated\n")
        self.write("core/internal/leaf/testdata/fixture.json", "{}\n")
        self.write("core/internal/leaf/testdata/golden.txt", "golden\n")
        self.write("core/internal/leaf/README.md", "embedded\n")
        self.write("core/docs/runbook.md", "docs\n")
        self.git("add", ".")
        self.git("commit", "-qm", "go module")
        self.git("update-ref", "refs/remotes/origin/main", "HEAD")

    def test_dependents_follow_imports_and_test_imports(self):
        self.write_go_module()
        self.assertEqual(checks.go_affected("core", {"core/internal/leaf/leaf.go"}),
                         (["./internal/leaf"], ["./internal/mid", "./internal/top"]))
        # A fixture belongs to the package whose directory encloses it.
        self.assertEqual(checks.go_affected("core", {"core/internal/leaf/testdata/fixture.json"}),
                         (["./internal/leaf"], ["./internal/mid", "./internal/top"]))
        # Test-only consumers are reached, and nothing unrelated is.
        self.assertEqual(checks.go_affected("core", {"core/internal/helper/helper.go"}),
                         (["./internal/helper"], ["./internal/top"]))
        self.assertEqual(checks.go_affected("core", {"core/internal/unrelated/unrelated.go"}),
                         (["./internal/unrelated"], []))
        # Golden files and embedded docs are build inputs of the package that
        # encloses them, whatever their suffix.
        for owned in ("core/internal/leaf/testdata/golden.txt", "core/internal/leaf/README.md"):
            self.assertEqual(checks.go_affected("core", {owned}),
                             (["./internal/leaf"], ["./internal/mid", "./internal/top"]), owned)
        # Documentation no package encloses runs nothing; anything else widens.
        self.assertEqual(checks.go_affected("core", {"core/docs/runbook.md"}), ([], []))
        self.assertIsNone(checks.go_affected("core", {"core/Dockerfile"}))
        self.assertIsNone(checks.go_affected("core", {"core/go.sum"}))

    def fake_dependents(self, returncode):
        class Started:
            pid = -12345

            def __init__(self):
                self.argv = None
                self.returncode = returncode
                self.output = io.BytesIO(b"ok  \tdependents\n")

            def wait(self):
                return self.returncode

        started = Started()

        def start(argv, **kwargs):
            started.argv = tuple(argv)
            return started

        return started, start

    def test_changed_packages_decide_the_result_before_dependents_are_reported(self):
        self.write_go_module()
        started, start = self.fake_dependents(0)
        with patch.object(checks, "run") as run, patch.object(checks, "start", start), \
                patch("sys.stdout", new_callable=io.StringIO) as out:
            checks.go_fast_tests("core", {"core/internal/leaf/leaf.go"}, False)
        self.assertEqual([call.args for call in run.call_args_list],
                         [("go", "test", "-short", "./internal/leaf")])
        self.assertEqual(started.argv, ("go", "test", "-short", "./internal/mid", "./internal/top"))
        self.assertIn("ok  \tdependents", out.getvalue())

    def test_a_failing_dependent_still_fails_the_run(self):
        self.write_go_module()
        started, start = self.fake_dependents(1)
        with patch.object(checks, "run"), patch.object(checks, "start", start), \
                patch("sys.stdout", new_callable=io.StringIO):
            with self.assertRaises(subprocess.CalledProcessError):
                checks.go_fast_tests("core", {"core/internal/leaf/leaf.go"}, False)

    def test_a_break_in_the_changed_package_terminates_the_dependents(self):
        self.write_go_module()
        started, start = self.fake_dependents(0)
        failure = subprocess.CalledProcessError(1, ("go", "test"))
        with patch.object(checks, "run", side_effect=failure), \
                patch.object(checks, "start", start), \
                patch.object(checks.os, "killpg") as killpg, \
                patch("sys.stdout", new_callable=io.StringIO):
            with self.assertRaises(subprocess.CalledProcessError):
                checks.go_fast_tests("core", {"core/internal/leaf/leaf.go"}, False)
        self.assertEqual(killpg.call_args.args[0], started.pid)

    def test_an_interrupt_while_waiting_for_dependents_terminates_them(self):
        self.write_go_module()
        started, start = self.fake_dependents(0)
        waits = []

        def wait():
            waits.append(1)
            if len(waits) == 1:
                raise KeyboardInterrupt
            return started.returncode

        started.wait = wait
        with patch.object(checks, "run"), patch.object(checks, "start", start), \
                patch.object(checks.os, "killpg") as killpg, \
                patch("sys.stdout", new_callable=io.StringIO):
            with self.assertRaises(KeyboardInterrupt):
                checks.go_fast_tests("core", {"core/internal/leaf/leaf.go"}, False)
        self.assertEqual(killpg.call_args.args[0], started.pid)

    def write_routes(self, path, routes):
        document = {"version": 1, "route_count": len(routes),
                    "routes": [{"method": m, "path": p, "access_class": c} for m, p, c in routes]}
        self.write(path, json.dumps(document) + "\n")

    def test_route_comparison_reports_additions_removals_and_reclassification(self):
        self.write_routes(checks.ROUTE_INVENTORY, [("GET", "/a", "public"), ("GET", "/b", "public")])
        self.git("add", ".")
        self.git("commit", "-qm", "routes")
        self.git("update-ref", "refs/remotes/origin/main", "HEAD")
        self.write_routes(checks.ROUTE_INVENTORY,
                          [("GET", "/a", "private"), ("POST", "/c", "public")])
        with patch("sys.stdout", new_callable=io.StringIO) as out:
            checks.route_changes()
        report = out.getvalue()
        self.assertIn("new route POST /c", report)
        self.assertIn("removed route GET /b", report)
        self.assertIn("reclassified GET /a: public -> private", report)
        # Advisory only: a route change must never block the commit.
        self.git("checkout", "--", checks.ROUTE_INVENTORY)
        with patch("sys.stdout", new_callable=io.StringIO) as out:
            checks.route_changes()
        self.assertIn("HTTP routes unchanged", out.getvalue())

    def test_route_comparison_skips_a_missing_baseline(self):
        with patch("sys.stdout", new_callable=io.StringIO) as out:
            checks.route_changes()
        self.assertIn("Could not compare", out.getvalue())

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
