import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stderr
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from anx_client import AnxError
from fleet_sync import (
    _finish, _quiet_status, apply_plans, cards_from_state, merge_known,
    observation_already_recorded, prune_cards, save_state, validate_text,
)


class _Client:
    def __init__(self, pages):
        self.pages = pages
        self.calls = 0

    def observations(self, ref, *, limit=50, cursor=""):
        self.calls += 1
        return self.pages.get(cursor, {"observations": [], "next_cursor": ""})

    def work_create(self, body):
        raise AssertionError("conflict recovery must not create")

    def observe(self, ref, body):
        raise AnxError("conflict", "work changed or idempotency key conflicts")


class ConflictTests(unittest.TestCase):
    def test_same_facts_digest_is_already_recorded(self):
        client = _Client({"": {"observations": [{"idempotency_key": "other"}], "next_cursor": "next"},
                          "next": {"observations": [{"idempotency_key": "abc"}], "next_cursor": ""}})
        self.assertTrue(observation_already_recorded(client, "card:1", "abc"))
        self.assertFalse(observation_already_recorded(client, "card:1", "missing"))

    def test_conflict_updates_cache_and_does_not_fail_the_run(self):
        plan = {
            "action": "observe", "create": False, "authority": "fleetctl", "connection_id": "fleet",
            "native_id": "host/mute", "title": "fleetctl: host/mute", "owner": "fleet", "digest": "abc",
            "reader_id": "fleet-sync/fleetctl", "observed_at": "2026-10-05T00:00:00Z",
            "facts": {"phase": "blocked", "title": "fleetctl: host/mute"}, "evidence": [],
            "card_ref": "card:1",
        }
        known = {("fleetctl", "fleet", "host/mute"): {"ref": "card:1", "digest": None, "title": "old", "owner": "fleet"}}
        client = _Client({"": {"observations": [{"idempotency_key": "abc"}], "next_cursor": ""}})
        with redirect_stderr(io.StringIO()) as err:
            summary = apply_plans(client, [plan], known)
        self.assertEqual(summary["errors"], [])
        self.assertEqual(summary["already_recorded"], 1)
        self.assertEqual(known[("fleetctl", "fleet", "host/mute")]["digest"], "abc")
        self.assertIn("already recorded", err.getvalue())
        with redirect_stderr(io.StringIO()) as quiet:
            code = _finish(summary, [{"name": "fleetctl", "ok": True}], quiet=True)
        self.assertEqual(code, 0)
        self.assertEqual(quiet.getvalue(), "")


class _Ok:
    def work_create(self, body):
        return {"work": {"ref": "card:new"}}

    def observe(self, ref, body):
        return {}


class _Lister:
    def __init__(self, cards):
        self.cards = cards

    def work_list(self, source):
        return self.cards


class StateTests(unittest.TestCase):
    def test_done_cards_older_than_30_days_are_pruned(self):
        now = datetime(2026, 10, 4, tzinfo=timezone.utc)
        plan = {
            "action": "observe", "create": False, "authority": "github", "connection_id": "github.com",
            "native_id": "example/repo#1", "title": "example/repo#1", "owner": "Operator", "digest": "d",
            "reader_id": "fleet-sync/github", "observed_at": "2026-08-01T00:00:00Z",
            "facts": {"phase": "done", "title": "example/repo#1"},
            "evidence": [{"ref": "fleet-sync:github:example/repo#1:absent"}],
            "card_ref": "card:1", "reason": "absent",
        }
        known = {("github", "github.com", "example/repo#1"): {"ref": "card:1", "digest": None, "title": "t", "owner": "Operator"}}
        apply_plans(_Ok(), [plan], known, now=datetime(2026, 8, 1, tzinfo=timezone.utc))
        self.assertEqual(known[("github", "github.com", "example/repo#1")]["done_at"], "2026-08-01T00:00:00Z")
        recent = {("github", "github.com", "example/repo#2"): {
            "ref": "card:2", "digest": "e", "title": "t", "owner": "Operator",
            "phase": "done", "done_at": "2026-09-20T00:00:00Z",
        }}
        known.update(recent)
        prune_cards(known, now=now)
        self.assertNotIn(("github", "github.com", "example/repo#1"), known)
        self.assertIn(("github", "github.com", "example/repo#2"), known)
        with tempfile.TemporaryDirectory() as directory:
            path = str(Path(directory) / "state.json")
            save_state(path, {"dashboard_ref": "doc:1", "history": []}, known, now=now)
            loaded = cards_from_state(json.loads(Path(path).read_text()))
        self.assertEqual(set(loaded), {("github", "github.com", "example/repo#2")})

    def test_merge_skips_a_card_done_for_more_than_30_days(self):
        cards = [{
            "ref": "card:old", "title": "old", "owner": "Operator", "phase": "done",
            "updated_at": "2026-01-01T00:00:00Z",
            "source": {"authority": "github", "connection_id": "github.com", "native_id": "example/repo#1"},
        }, {
            "ref": "card:open", "title": "open", "owner": "Operator", "phase": "review",
            "updated_at": "2026-10-01T00:00:00Z",
            "source": {"authority": "github", "connection_id": "github.com", "native_id": "example/repo#2"},
        }]
        known = {}
        merge_known(_Lister(cards), known, ["github"], now=datetime(2026, 10, 4, tzinfo=timezone.utc))
        self.assertNotIn(("github", "github.com", "example/repo#1"), known)
        self.assertIn(("github", "github.com", "example/repo#2"), known)

    def test_open_phase_records_seen_at(self):
        now = datetime(2026, 10, 4, tzinfo=timezone.utc)
        plan = {
            "action": "skip", "authority": "github", "connection_id": "github.com",
            "native_id": "example/repo#3", "title": "example/repo#3", "owner": "Operator",
            "digest": "d", "reader_id": "fleet-sync/github", "observed_at": "2026-10-04T00:00:00Z",
            "facts": {"phase": "review", "title": "example/repo#3"}, "evidence": [],
        }
        known = {("github", "github.com", "example/repo#3"): {
            "ref": "card:3", "digest": "d", "title": "t", "owner": "Operator", "phase": "review",
        }}
        apply_plans(_Ok(), [plan], known, now=now)
        self.assertEqual(known[("github", "github.com", "example/repo#3")]["seen_at"], "2026-10-04T00:00:00Z")
        with tempfile.TemporaryDirectory() as directory:
            path = str(Path(directory) / "state.json")
            save_state(path, {}, known, now=now)
            loaded = cards_from_state(json.loads(Path(path).read_text(encoding="utf-8")))
        self.assertEqual(loaded[("github", "github.com", "example/repo#3")]["seen_at"], "2026-10-04T00:00:00Z")
        self.assertEqual(loaded[("github", "github.com", "example/repo#3")]["phase"], "review")

    def test_merge_copies_terminal_phase_and_open_seen_at(self):
        cards = [{
            "ref": "card:done", "title": "done", "owner": "Operator", "phase": "done",
            "updated_at": "2026-10-01T00:00:00Z",
            "latest_observation": {"observed_at": "2026-10-01T00:00:00Z"},
            "source": {"authority": "github", "connection_id": "github.com", "native_id": "aaa/repo#1"},
        }, {
            "ref": "card:open", "title": "open", "owner": "Operator", "phase": "review",
            "updated_at": "2026-10-03T00:00:00Z",
            "source": {"authority": "github", "connection_id": "github.com", "native_id": "example/repo#9"},
        }]
        known = {}
        merge_known(_Lister(cards), known, ["github"], now=datetime(2026, 10, 4, tzinfo=timezone.utc))
        self.assertEqual(known[("github", "github.com", "aaa/repo#1")]["phase"], "done")
        self.assertNotIn("seen_at", known[("github", "github.com", "aaa/repo#1")])
        self.assertEqual(known[("github", "github.com", "example/repo#9")]["seen_at"], "2026-10-03T00:00:00Z")


class QuietTests(unittest.TestCase):
    def test_reader_failure_is_one_line_and_nonzero(self):
        reads = [
            {"name": "prometheus", "ok": False, "error": "timed out"},
            {"name": "fleetctl", "ok": True},
        ]
        with redirect_stderr(io.StringIO()) as err:
            code = _quiet_status(reads, valid=True, diagnostics=[], errors=[])
        self.assertEqual(code, 1)
        self.assertEqual(err.getvalue().strip(), "prometheus: timed out")
        with redirect_stderr(io.StringIO()) as err:
            code = _quiet_status(reads, valid=False, diagnostics=["panels: bad"], errors=[])
        self.assertEqual(code, 1)
        self.assertIn("report failed validation", err.getvalue())
        self.assertIn("prometheus: timed out", err.getvalue())

    def test_missing_node_binary_is_named(self):
        ok, errors = validate_text("{}", node_bin="/no/such/node")
        self.assertFalse(ok)
        self.assertIn("/no/such/node", errors[0])


if __name__ == "__main__":
    unittest.main()
