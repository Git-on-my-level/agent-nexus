import io
import sys
import unittest
from contextlib import redirect_stderr
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from anx_client import AnxError
from fleet_sync import _finish, _quiet_status, apply_plans, observation_already_recorded, validate_text


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
