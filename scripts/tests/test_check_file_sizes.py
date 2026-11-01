"""File-size ratchet regressions: what counts, baseline rules, shrink-only writes."""

import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

# A git hook exports GIT_DIR and friends, which would point the checkers' own
# git calls at the real repository instead of these fixtures.
for _name in [name for name in os.environ if name.startswith("GIT_")]:
    del os.environ[_name]


spec = importlib.util.spec_from_file_location(
    "check_file_sizes", Path(__file__).resolve().parents[1] / "check-file-sizes.py")
check_file_sizes = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check_file_sizes)


class RatchetTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.name", "ratchet tests")
        self.git("config", "user.email", "ratchet@example.test")
        self.baseline = self.root / "scripts/file-size-baseline.json"
        self.baseline.parent.mkdir()
        patcher = patch.object(check_file_sizes, "BASELINE", self.baseline)
        patcher.start()
        self.addCleanup(patcher.stop)

    def git(self, *args):
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_SYSTEM=os.devnull)
        return subprocess.check_output(["git", *args], cwd=self.root,
                                       stderr=subprocess.PIPE, env=env)

    def write(self, path, lines):
        file = self.root / path
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text("".join(line + "\n" for line in lines))
        return path

    def commit(self):
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")

    def set_baseline(self, entries):
        self.baseline.write_text(json.dumps(entries, indent=2, sort_keys=True) + "\n")

    def oversized(self, path):
        return check_file_sizes.oversized(self.root).get(path)

    def test_only_handwritten_sources_carry_the_ratchet(self):
        big = "x\n" * 900
        for path in ("core/big.go", "core/skip_test.go", "core/gen_generated.go",
                     "core/queries_gen.go", "contracts/gen/big.js",
                     "web-ui/src/lib/generated/big.json", "web-ui/big.py",
                     "web-ui/big.spec.ts", "web-ui/big.md", "web-ui/Makefile"):
            file = self.root / path
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_text(big)
        self.commit()
        self.assertEqual(check_file_sizes.oversized(self.root),
                         {"core/big.go": 900, "web-ui/big.py": 900})

    def test_line_counts_match_wc(self):
        # A file whose last line has no trailing newline counts its newlines,
        # exactly like `wc -l`.
        path = "core/count.go"
        (self.root / path).parent.mkdir(parents=True, exist_ok=True)
        (self.root / path).write_text("line\nline\nline")
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")
        wc = int(subprocess.check_output(["wc", "-l", str(self.root / path)]).split()[0])
        self.assertEqual(check_file_sizes.line_count(self.root, path), wc)
        self.assertEqual(wc, 2)

    def test_check_passes_when_the_baseline_matches(self):
        self.write("core/big.go", ["x"] * 810)
        self.commit()
        self.set_baseline({"core/big.go": 810})
        with patch("sys.stdout", new_callable=__import__("io").StringIO):
            check_file_sizes.check(self.root)

    def test_an_unbaselined_over_limit_file_must_be_split(self):
        self.write("core/big.go", ["x"] * 810)
        self.commit()
        self.set_baseline({})
        with self.assertRaises(SystemExit) as failure:
            check_file_sizes.check(self.root)
        self.assertIn("split core/big.go by concern before adding code", str(failure.exception))
        self.assertIn("--allow-growth", str(failure.exception))

    def test_a_baselined_file_may_not_grow(self):
        self.write("core/big.go", ["x"] * 820)
        self.commit()
        self.set_baseline({"core/big.go": 810})
        with self.assertRaises(SystemExit) as failure:
            check_file_sizes.check(self.root)
        self.assertIn("grew from 810 to 820", str(failure.exception))
        self.assertIn("split core/big.go by concern", str(failure.exception))

    def test_shrinking_or_losing_an_entry_demands_a_refresh(self):
        for name, lines, expected in (("core/big.go", ["x"] * 805, "shrank to 805 lines"),
                                      ("core/big.go", ["x"] * 90, "dropped under the limit")):
            self.write(name, lines)
            self.git("add", ".")
            self.git("commit", "-qm", "fixture")
            self.set_baseline({"core/big.go": 810, "core/gone.go": 830})
            with self.assertRaises(SystemExit) as failure:
                check_file_sizes.check(self.root)
            message = str(failure.exception)
            self.assertIn(f"baseline entry core/big.go (recorded 810 lines) {expected}",
                          message)
            self.assertIn("baseline entry core/gone.go (recorded 830 lines) is gone", message)
            self.assertIn("run make file-size-baseline", message)

    def test_write_lowers_and_drops_but_never_grows_without_a_reason(self):
        self.write("core/big.go", ["x"] * 805)
        self.commit()
        self.set_baseline({"core/big.go": 810, "core/gone.go": 830})
        with patch("sys.stdout", new_callable=__import__("io").StringIO):
            check_file_sizes.write(self.root, None)
        self.assertEqual(json.loads(self.baseline.read_text()), {"core/big.go": 805})
        self.write("core/new-big.js", ["x"] * 850)
        self.commit()
        with self.assertRaises(SystemExit) as refusal:
            check_file_sizes.write(self.root, None)
        self.assertIn("Refusing to grow the file-size baseline", str(refusal.exception))
        self.assertIn("core/new-big.js would be added at 850 lines",
                      str(refusal.exception))
        with patch("sys.stdout", new_callable=__import__("io").StringIO) as out:
            check_file_sizes.write(self.root, "landing the widget parser whole")
        recorded = json.loads(self.baseline.read_text())
        self.assertEqual(recorded["core/new-big.js"], 850)
        report = out.getvalue()
        self.assertIn("file-size baseline growth: landing the widget parser whole", report)
        self.assertIn("added core/new-big.js at 850 lines", report)


if __name__ == "__main__":
    unittest.main()
