"""Coverage for the per-entry merge of generated JSON inventories."""

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import unittest.mock


spec = importlib.util.spec_from_file_location(
    "json_inventory_merge", Path(__file__).with_name("json_inventory_merge.py"))
driver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(driver)
DRIVER = Path(__file__).with_name("json_inventory_merge.py").resolve()
INVENTORY = "core/internal/storage/testdata/resource_access_storage.json"


def inventory(tables, writers=None):
    return {"tables": tables, "writers": writers or {}, "external_key_publications": {}}


class MergeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def write(self, name, document):
        path = self.root / name
        path.write_text(json.dumps(document, indent=2) + "\n")
        return path

    def merge(self, base, ours, theirs):
        files = [self.write(name, document) for name, document in
                 (("base", base), ("ours", ours), ("theirs", theirs))]
        status = driver.main([str(path) for path in files] + [INVENTORY])
        return status, json.loads(files[1].read_text())

    def test_disjoint_additions_merge_without_conflict(self):
        base = inventory({"work": {"id": "identity"}}, {"store.go:Insert": "aaa"})
        ours = inventory({"work": {"id": "identity", "body_json": "scope"}},
                         {"store.go:Insert": "aaa", "store.go:Update": "bbb"})
        theirs = inventory({"work": {"id": "identity", "title": "scope"},
                            "series": {"id": "identity"}},
                           {"store.go:Insert": "aaa"})
        status, merged = self.merge(base, ours, theirs)
        self.assertEqual(status, 0)
        self.assertEqual(merged["tables"], {"series": {"id": "identity"},
                                            "work": {"body_json": "scope", "id": "identity",
                                                     "title": "scope"}})
        self.assertEqual(merged["writers"], {"store.go:Insert": "aaa", "store.go:Update": "bbb"})

    def test_same_entry_with_different_values_conflicts(self):
        base = inventory({"work": {"body_json": "scope"}})
        ours = inventory({"work": {"body_json": "identity"}})
        theirs = inventory({"work": {"body_json": "internal:schema"}})
        files = [self.write(name, document) for name, document in
                 (("base", base), ("ours", ours), ("theirs", theirs))]
        self.assertEqual(driver.main([str(path) for path in files] + [INVENTORY]), 1)
        # A refused merge must stay unresolvable: markers, both classifications,
        # and nothing that a stray `git add` would mistake for a resolution.
        conflicted = files[1].read_text()
        self.assertIn("<<<<<<< ours", conflicted)
        self.assertIn(">>>>>>> theirs", conflicted)
        self.assertIn("identity", conflicted)
        self.assertIn("internal:schema", conflicted)
        with self.assertRaises(ValueError):
            json.loads(conflicted)

    def assert_refusal_cannot_be_resolved_blindly(self, base, ours, theirs):
        """A refused merge must leave something no `git add` can accept.

        Both relocating an entry and both adding it land the two sides' edits in
        disjoint line regions, so the fallback line merge succeeds and yields
        duplicate keys: valid JSON that prettier calls unchanged and whose
        decoders (Python's and Go's alike) keep only the last of the two. That
        is a conflict resolved by silently dropping a classification, which is
        why the driver has to mark both sides itself.
        """
        files = [self.write(name, document) for name, document in
                 (("base", base), ("ours", ours), ("theirs", theirs))]
        line_merge = subprocess.run(["git", "merge-file", "-p", "--diff3",
                                     str(files[1]), str(files[0]), str(files[2])],
                                    capture_output=True)
        clean = b"<<<<<<<" not in line_merge.stdout
        self.assertEqual(driver.main([str(path) for path in files] + [INVENTORY]), 1)
        conflicted = files[1].read_text()
        self.assertIn("<<<<<<< ours", conflicted)
        self.assertIn(">>>>>>> theirs", conflicted)
        for classification in ("scope", "internal:schema"):
            self.assertIn(classification, conflicted)
        with self.assertRaises(ValueError):
            json.loads(conflicted)
        return clean

    def test_relocating_an_entry_on_both_sides_cannot_be_resolved_blindly(self):
        # The reviewed reproduction: both branches move `actors` somewhere else
        # and classify `actors.id` differently.
        def tables(order, classification):
            rows = {"mmm_middle": {"id": "identity"}, "zz_last": {"id": "identity"},
                    "actors": {"id": classification}}
            return {name: rows[name] for name in order}

        base = inventory(tables(("actors", "mmm_middle", "zz_last"), "identity"))
        ours = inventory(tables(("mmm_middle", "zz_last", "actors"), "scope"))
        theirs = inventory(tables(("mmm_middle", "actors", "zz_last"), "internal:schema"))
        self.assertTrue(self.assert_refusal_cannot_be_resolved_blindly(base, ours, theirs),
                        "fixture no longer reproduces a clean line merge")

    def test_adding_an_entry_on_both_sides_cannot_be_resolved_blindly(self):
        base = inventory({"mmm_middle": {"id": "identity"}})
        ours = inventory({"actors": {"id": "scope"}, "mmm_middle": {"id": "identity"}})
        theirs = inventory({"mmm_middle": {"id": "identity"},
                            "actors": {"id": "internal:schema"}})
        self.assertTrue(self.assert_refusal_cannot_be_resolved_blindly(base, ours, theirs),
                        "fixture no longer reproduces a clean line merge")

    def test_markers_stay_on_their_own_line_without_a_trailing_newline(self):
        for name, body in (("base", "{}"), ("ours", "{}\n"), ("theirs", "{}")):
            (self.root / name).write_text(body)
        marked = driver.marked(*(self.root / name for name in ("base", "ours", "theirs")))
        self.assertEqual(marked.decode().splitlines(),
                         ["<<<<<<< ours", "{}", "||||||| base", "{}",
                          "=======", "{}", ">>>>>>> theirs"])

    def test_unparseable_input_hands_back_a_conflict(self):
        files = [self.write("base", inventory({})), self.write("ours", inventory({})),
                 self.write("theirs", inventory({}))]
        files[2].write_text("{not json")
        self.assertEqual(driver.main([str(path) for path in files] + [INVENTORY]), 1)

    def test_unavailable_formatter_hands_back_a_conflict(self):
        base = inventory({"work": {"id": "identity"}})
        ours = inventory({"work": {"id": "identity", "body_json": "scope"}})
        theirs = inventory({"work": {"id": "identity", "title": "scope"}})
        files = [self.write(name, document) for name, document in
                 (("base", base), ("ours", ours), ("theirs", theirs))]
        with unittest.mock.patch.object(driver, "format_with_prettier", lambda target: False):
            self.assertEqual(driver.main([str(path) for path in files] + [INVENTORY]), 1)
        # Never leave unformatted generated output behind for a later commit.
        self.assertIn("<<<<<<< ours", files[1].read_text())

    def test_identical_edits_and_regenerated_hashes_are_not_conflicts(self):
        base = inventory({}, {"store.go:Insert": "aaa"})
        same = inventory({}, {"store.go:Insert": "bbb"})
        status, merged = self.merge(base, same, same)
        self.assertEqual((status, merged["writers"]), (0, {"store.go:Insert": "bbb"}))

    def test_deletions_apply_once_and_conflict_against_an_edit(self):
        base = inventory({"work": {"id": "identity", "dropped": "scope"}})
        removed = inventory({"work": {"id": "identity"}})
        status, merged = self.merge(base, removed, base)
        self.assertEqual((status, merged["tables"]), (0, {"work": {"id": "identity"}}))
        reclassified = inventory({"work": {"id": "identity", "dropped": "identity"}})
        files = [self.write(name, document) for name, document in
                 (("base", base), ("ours", removed), ("theirs", reclassified))]
        self.assertEqual(driver.main([str(path) for path in files] + [INVENTORY]), 1)

    def test_merged_output_matches_the_committed_formatting(self):
        committed = Path(__file__).resolve().parents[2] / INVENTORY
        if not committed.is_file():
            self.skipTest("inventory not present in this checkout")
        original = json.loads(committed.read_bytes())
        ours = json.loads(json.dumps(original))
        ours["tables"]["anx_schema_version"] = {"version": "internal:schema"}
        files = [self.write("base", original), self.write("ours", ours),
                 self.write("theirs", original)]
        self.assertEqual(driver.main([str(path) for path in files] + [INVENTORY]), 0)
        # Byte-for-byte, so the commit hook's prettier check stays green and the
        # diff shows only the merged entries.
        reformatted = self.root / "reformatted.json"
        reformatted.write_text(files[1].read_text())
        subprocess.run(["pnpm", "-C", "web-ui", "exec", "prettier", "--write",
                        "--parser=json", str(reformatted)],
                       cwd=Path(__file__).resolve().parents[2], check=True, capture_output=True)
        self.assertEqual(reformatted.read_bytes(), files[1].read_bytes())
        self.assertEqual(list(json.loads(files[1].read_text())), list(original))

    def test_rebasing_two_branches_that_classify_different_columns(self):
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_SYSTEM=os.devnull)

        def git(*args):
            return subprocess.run(["git", *args], cwd=self.root, env=env, check=False,
                                  capture_output=True, text=True)

        git("init", "-q", "-b", "main")
        git("config", "user.name", "Merge driver tests")
        git("config", "user.email", "hooks@example.test")
        git("config", "commit.gpgsign", "false")
        git("config", "merge.anx-json-inventory.driver",
            f"python3 -B {DRIVER} %O %A %B %P")
        (self.root / ".gitattributes").write_text(f"{INVENTORY} merge=anx-json-inventory\n")
        (self.root / INVENTORY).parent.mkdir(parents=True)
        self.write(INVENTORY, inventory({"work": {"id": "identity"}}))
        git("add", ".")
        git("commit", "-qm", "base")
        for branch, column in (("one", "body_json"), ("two", "title")):
            git("checkout", "-q", "-b", branch, "main")
            document = inventory({"work": {"id": "identity", column: "scope"}})
            self.write(INVENTORY, document)
            git("commit", "-qam", branch)
        rebase = git("rebase", "main", "two")
        git("rebase", "--abort")
        self.assertEqual(rebase.returncode, 0, rebase.stdout + rebase.stderr)
        git("checkout", "-q", "one")
        merge = git("merge", "--no-edit", "two")
        self.assertEqual(merge.returncode, 0, merge.stdout + merge.stderr)
        merged = json.loads((self.root / INVENTORY).read_text())
        self.assertEqual(merged["tables"],
                         {"work": {"body_json": "scope", "id": "identity", "title": "scope"}})


if __name__ == "__main__":
    unittest.main()
