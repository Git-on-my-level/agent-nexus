import copy
from datetime import datetime, timedelta, timezone
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("tracking_migration", Path(__file__).resolve().parents[1] / "migrate-tracking-cadence.py")
migration = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(migration)
NOW = datetime(2026, 10, 25, 12, tzinfo=timezone.utc)


def receipts(gaps):
    at = NOW - timedelta(days=10)
    result = [{"at": at.isoformat(), "event_id": "0", "run_id": "0"}]
    for gap in gaps:
        at += timedelta(seconds=gap)
        result.append({"at": at.isoformat(), "event_id": str(len(result)), "run_id": str(len(result))})
    return result


class FakeANX:
    def __init__(self):
        self.work = {"id": "one", "ref": "card:one", "title": "One", "owner": "actor:owner", "thread_id": "thread-one", "version": 1, "phase": "in_progress"}
        self.rows = receipts([3600, 3600])
        self.writes = []
        self.empty_first = False
        self.uncertain = False

    def __call__(self, args):
        if args == ["config", "workspaces"]:
            return {"workspaces": [{"alias": "test", "workspace_id": "workspace-one", "base_url": "https://example.test/workspace"}]}
        operation = args[1]
        params = dict(args[i + 1].split("=", 1) for i, arg in enumerate(args) if arg == "--param")
        if operation == "workList":
            if self.empty_first and "cursor" not in params:
                return {"work": [], "next_cursor": "next-page"}
            return {"work": [copy.deepcopy(self.work)], "next_cursor": ""}
        if operation == "workGet":
            return {"work": copy.deepcopy(self.work)}
        if operation == "listEvents":
            assert params["limit"] == "50"
            return {"events": [{"id": row["event_id"], "ts": row["at"], "subject_ref": "card:one", "payload": {"run_id": row["run_id"]}, **{key: row[key] for key in ("archived_at", "trashed_at") if key in row}} for row in self.rows], "page_info": {"has_more": False}}
        if operation == "workPatch":
            body = json.loads(args[args.index("--json-body") + 1])
            self.writes.append(body)
            assert body["if_version"] == self.work["version"]
            self.work["cadence"] = copy.deepcopy(body["patch"]["cadence"])
            self.work["version"] += 1
            if self.uncertain:
                raise OSError("connection lost after commit")
            return {"work": copy.deepcopy(self.work)}
        raise AssertionError(args)


class TrackingMigrationTest(unittest.TestCase):
    def test_lifecycle_receipts_do_not_make_a_cadence(self):
        for lifecycle in ("archived_at", "trashed_at"):
            with self.subTest(lifecycle=lifecycle):
                fake = FakeANX()
                fake.rows[1][lifecycle] = NOW.isoformat()
                tool = migration.Migration("test", "owner", fake)
                entry = tool.report(NOW)["cards"][0]
                self.assertEqual(entry["action"], "owner_review")
                self.assertEqual(entry["reason"], "fewer_than_three_receipts")
                self.assertEqual(len(entry["receipts"]), 2)
                fake.rows[1][lifecycle] = None  # Restored/unarchived receipts count again.
                self.assertEqual(tool.report(NOW)["cards"][0]["every_seconds"], 3600)

    def test_receipt_lifecycle_change_invalidates_apply(self):
        for lifecycle in ("archived_at", "trashed_at"):
            with self.subTest(lifecycle=lifecycle), tempfile.TemporaryDirectory() as directory:
                fake = FakeANX()
                tool = migration.Migration("test", "owner", fake)
                report = tool.report(NOW)
                fake.rows[1][lifecycle] = NOW.isoformat()
                result = tool.apply(report, Path(directory) / "apply.json", NOW)
                self.assertEqual(result["cards"]["card:one"]["status"], "conflict")
                self.assertEqual(fake.writes, [])

    def test_median_and_spread_are_deterministic(self):
        self.assertEqual(migration.infer(receipts([3600, 3600])), (3600, "regular_receipts"))
        self.assertEqual(migration.infer(receipts([3600, 3602, 3604, 3606])), (3603, "regular_receipts"))
        self.assertEqual(migration.infer(receipts([3600, 4400]))[0], 4000)
        self.assertEqual(migration.infer(receipts([3600, 4401]))[1], "irregular_intervals")
        self.assertEqual(migration.infer(receipts([3600]))[1], "fewer_than_three_receipts")
        self.assertIsNone(migration.infer(receipts([0, 0]))[0])
        self.assertIsNone(migration.infer(receipts([10, 10]))[0])
        with self.assertRaises(ValueError):
            migration.stamp("2026-10-25T12:00:00")

    def test_report_is_read_only_and_follows_empty_cursor_pages(self):
        fake = FakeANX()
        fake.empty_first = True
        tool = migration.Migration("test", "owner", fake)
        report = tool.report(NOW)
        self.assertEqual(len(report["cards"]), 1)
        self.assertEqual(report["cards"][0]["action"], "infer")
        self.assertEqual(report["cards"][0]["every_seconds"], 3600)
        self.assertEqual(fake.writes, [])
        fake.rows = receipts([3600, 20000])
        self.assertEqual(tool.report(NOW)["cards"][0]["action"], "owner_review")

    def test_apply_requires_same_workspace_and_unchanged_sample(self):
        fake = FakeANX()
        tool = migration.Migration("test", "owner", fake)
        report = tool.report(NOW)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "apply.json"
            wrong = copy.deepcopy(report)
            wrong["workspace"]["workspace_id"] = "other"
            with self.assertRaises(ValueError):
                tool.apply(wrong, path, NOW)
            fake.rows = receipts([3600, 3601])
            journal = tool.apply(report, path, NOW)
            self.assertEqual(journal["cards"]["card:one"]["status"], "conflict")
            self.assertEqual(fake.writes, [])

    def test_apply_preserves_concurrent_metadata_and_existing_cadence(self):
        for change in ("version", "cadence"):
            fake = FakeANX()
            tool = migration.Migration("test", "owner", fake)
            report = tool.report(NOW)
            if change == "version":
                fake.work["version"] = 2
            else:
                fake.work["cadence"] = {"every_seconds": 7200}
            with tempfile.TemporaryDirectory() as directory:
                journal = tool.apply(report, Path(directory) / "apply.json", NOW)
                self.assertEqual(journal["cards"]["card:one"]["status"], "conflict")
                self.assertEqual(fake.writes, [])

    def test_persistence_failure_prevents_send_and_uncertain_replay_is_safe(self):
        fake = FakeANX()
        tool = migration.Migration("test", "owner", fake)
        report = tool.report(NOW)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "apply.json"
            with patch.object(migration, "atomic_json", side_effect=OSError("disk full")):
                with self.assertRaises(OSError):
                    tool.apply(report, path, NOW)
            self.assertEqual(fake.writes, [])
            fake.uncertain = True
            with self.assertRaises(OSError):
                tool.apply(report, path, NOW)
            pending = json.loads(path.read_text())
            self.assertEqual(pending["cards"]["card:one"]["status"], "uncertain")
            self.assertEqual(pending["cards"]["card:one"]["request"], {"if_version": 1, "patch": {"cadence": {"every_seconds": 3600}}})
            journal = tool.apply(report, path, NOW)
            self.assertEqual(journal["cards"]["card:one"]["status"], "matching_cadence_observed")
            self.assertEqual(len(fake.writes), 1)

    def test_dry_run_file_is_exclusive(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "report.json"
            migration.atomic_json(path, {"first": True}, exclusive=True)
            with self.assertRaises(FileExistsError):
                migration.atomic_json(path, {"first": False}, exclusive=True)
            self.assertEqual(json.loads(path.read_text()), {"first": True})


if __name__ == "__main__":
    unittest.main()
