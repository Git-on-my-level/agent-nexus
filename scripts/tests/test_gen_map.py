"""Map generator regressions: determinism, descriptions, package rows, staleness."""

import importlib.util
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "gen_map", Path(__file__).resolve().parents[1] / "gen-map.py")
gen_map = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gen_map)


class MapTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.name", "map tests")
        self.git("config", "user.email", "map@example.test")
        self.write("README.md",
                   "# Repo\n\nTop monorepo of widgets. More paragraph text follows later.\n\n"
                   "## Layout\n\n- `core/`: widget core\n")
        self.write("core/README.md", "# Core\n\nWidget core server and storage. Second sentence.\n")
        self.write("core/internal/store/store.go",
                   "// Package store keeps widgets. It persists them.\npackage store\n")
        self.write("core/internal/store/queries.go", "package store\n")
        self.write("core/internal/store/store_test.go", "package store\n")
        self.write("core/internal/quiet/quiet.go", "package quiet\n")
        self.write("web-ui/src/lib/client.js", "export {}\n")
        self.write("web-ui/src/routes/+page.svelte", "<main />\n")
        self.write("adapters/thing.py", "\n" * 30)
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")

    def git(self, *args):
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_SYSTEM=os.devnull)
        return subprocess.check_output(["git", *args], cwd=self.root,
                                       stderr=subprocess.PIPE, env=env)

    def write(self, path, text):
        file = self.root / path
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(text)

    def row(self, rendered, first_cell):
        """The stripped cells of the one table row whose first cell matches."""
        marker = f"| {first_cell} "
        found = [line for line in rendered.splitlines() if line.startswith(marker)]
        self.assertEqual(len(found), 1, f"expected one row for {first_cell}")
        return [cell.strip() for cell in found[0].strip().strip("|").split("|")]

    def test_render_is_deterministic(self):
        self.assertEqual(gen_map.render(self.root), gen_map.render(self.root))

    def test_module_rows_read_the_readme_first_paragraph(self):
        rendered = gen_map.render(self.root)
        self.assertEqual(self.row(rendered, "core"),
                         ["core", "Widget core server and storage."])
        # adapters has no README or AGENTS of its own.
        self.assertEqual(self.row(rendered, "adapters"), ["adapters", "(undocumented)"])
        self.assertIn("# Repository map", rendered)
        self.assertIn("Regenerate with `make map`", rendered)

    def test_go_package_rows_use_the_doc_comment_and_non_test_counts(self):
        rendered = gen_map.render(self.root)
        self.assertEqual(self.row(rendered, "core/internal/store"),
                         ["core/internal/store", "2", "Package store keeps widgets."])
        self.assertEqual(self.row(rendered, "core/internal/quiet"),
                         ["core/internal/quiet", "1", "(undocumented)"])
        # testdata directories are invisible to the go tool, so not packages.
        self.assertNotIn("testdata", rendered)

    def test_web_ui_areas_and_large_files_come_from_the_ratchet_logic(self):
        self.write("web-ui/src/lib/huge.js", "x\n" * (gen_map.check_file_sizes.LIMIT + 5))
        self.git("add", ".")
        self.git("commit", "-qm", "huge")
        rendered = gen_map.render(self.root)
        self.assertEqual(self.row(rendered, "web-ui/src/lib"),
                         ["web-ui/src/lib", "(undocumented)"])
        self.assertIn("(undocumented)", self.row(rendered, "web-ui/src/routes"))
        self.assertIn("## large files, split before extending", rendered)
        self.assertEqual(self.row(rendered, str(gen_map.check_file_sizes.LIMIT + 5)),
                         [str(gen_map.check_file_sizes.LIMIT + 5), "web-ui/src/lib/huge.js"])

    def test_first_sentence_does_not_cut_versions_or_colons(self):
        first = gen_map.first_sentence
        self.assertEqual(first("Released v0.13.51 today. Next up."), "Released v0.13.51 today.")
        self.assertEqual(first("Enroll each machine once:"), "Enroll each machine once")
        self.assertEqual(first(""), "(undocumented)")

    def test_check_mode_compares_without_writing(self):
        output = self.root / "docs/map.md"
        patchers = [patch.object(gen_map, "ROOT", self.root),
                    patch.object(gen_map, "OUTPUT", output)]
        for patcher in patchers:
            patcher.start()
            self.addCleanup(patcher.stop)
        with patch.object(sys, "argv", ["gen-map.py"]), \
                patch("sys.stdout", new_callable=io.StringIO):
            gen_map.main()  # write
        with patch.object(sys, "argv", ["gen-map.py", "--check"]), \
                patch("sys.stdout", new_callable=io.StringIO):
            gen_map.main()  # compare clean
        output.write_text("hand-edited\n")
        with patch.object(sys, "argv", ["gen-map.py", "--check"]):
            with self.assertRaises(SystemExit) as failure:
                gen_map.main()
        self.assertIn("docs/map.md is stale; run make map", str(failure.exception))


if __name__ == "__main__":
    unittest.main()
