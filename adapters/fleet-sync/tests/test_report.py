import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from report import build_report, changed_besides_generated_at

NOW = datetime(2026, 10, 4, 12, 0, tzinfo=timezone.utc)
ROOT = Path(__file__).resolve().parents[3]


def _reads():
    return [
        {
            "name": "multica", "ok": True, "complete": True, "observed_at": "2026-10-04T11:00:00Z",
            "items": [{
                "native_id": "11111111-1111-1111-1111-111111111111",
                "identifier": "EX-1", "title": "reply OK review", "status": "in_review",
                "updated_at": "2026-10-01T00:00:00Z", "host": "Example Host",
                "url": "https://multica.example.invalid/acme/issues/11111111-1111-1111-1111-111111111111",
                "signals": ["reply-ok"], "in_review_over_72h": True, "assignee_name": "Example",
            }],
            "meta": {"workspace_url": "https://multica.example.invalid/acme/issues"},
        },
        {
            "name": "github", "ok": True, "complete": True, "observed_at": "2026-10-04T11:05:00Z",
            "items": [{
                "native_id": "example/repo#4", "repo": "example/repo", "title": "Fix the gate",
                "url": "https://github.com/example/repo/pull/4", "state": "OPEN", "is_draft": False,
                "ci": "red", "updated_at": "2026-10-04T01:00:00Z", "created_at": "2026-09-01T00:00:00Z",
                "terminal": False, "review_decision": "CHANGES_REQUESTED",
            }],
            "meta": {},
        },
        {
            "name": "hermes", "ok": False, "complete": False, "observed_at": "2026-10-04T11:06:00Z",
            "error": "remote-host: ssh remote-host failed: timed out", "items": [],
            "meta": {"hosts": [{
                "label": "remote-host", "ok": False, "error": "ssh remote-host failed: timed out",
                "jobs": None, "rows": [],
            }]},
        },
        {
            "name": "agentctl", "ok": True, "complete": True, "observed_at": "2026-10-04T11:07:00Z",
            "items": [],
            "meta": {"hosts": [{
                "label": "local-host", "ok": True, "stuck": 1, "uncollected": 4,
                "live_running": 2, "version": "v0.0.0", "over_threshold": True,
            }]},
        },
        {
            "name": "fleetctl", "ok": True, "complete": True, "observed_at": "2026-10-04T11:08:00Z",
            "items": [{"native_id": "host/mute"}],
            "meta": {
                "queues": {"host-action": 1, "observability": 0, "intent": 2},
                "inventory": {"reporting": 1, "mute": 1, "asleep": 0},
                "hosts": {"local-host": "reporting"},
            },
        },
    ]


class ReportTests(unittest.TestCase):
    def test_unknown_stays_null_and_failure_is_visible(self):
        report = build_report(
            _reads(), generated_at="2026-10-04T12:00:00Z", now=NOW,
            hosts=[{"label": "local-host", "local": True}, {"label": "remote-host", "ssh": "remote-host"}],
        )
        self.assertEqual(report["kind"], "anx.visual-report")
        self.assertEqual([item["label"] for item in report["layout"]["items"]],
                         ["Overview", "Agent work", "Pull requests", "Ops & crons", "Hosts"])
        unavailable = next(panel for panel in report["panels"] if panel["id"] == "unavailable-hermes")
        self.assertEqual(unavailable["freshness"], "unavailable")
        self.assertIn("hermes", unavailable["data"]["text"])
        strip = next(panel for panel in report["panels"] if panel["id"] == "overview-now")
        cron = next(item for item in strip["data"]["items"] if item["label"] == "Cron problems")
        self.assertEqual(cron["value"], "unknown")
        self.assertNotEqual(cron["value"], "0")
        queues = next(panel for panel in report["panels"] if panel["id"] == "fleetctl-queues")
        self.assertIsNone(queues["data"]["option"]["series"][0]["data"][3])
        self.assertTrue(all(source.get("url", "").startswith("https://") for source in report["sources"]))
        same = json.loads(json.dumps(report))
        same["generated_at"] = "2026-10-05T00:00:00Z"
        self.assertFalse(changed_besides_generated_at(json.dumps(report), same))
        same["title"] = "Other"
        self.assertTrue(changed_besides_generated_at(json.dumps(report), same))

    @unittest.skipUnless(shutil.which("node"), "node is not installed")
    def test_validator_accepts_report(self):
        report = build_report(
            _reads(), generated_at="2026-10-04T12:00:00Z", now=NOW,
            hosts=[{"label": "local-host", "local": True}, {"label": "remote-host", "ssh": "remote-host"}],
        )
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
            json.dump(report, handle)
            path = handle.name
        completed = subprocess.run(
            ["node", str(ROOT / "web-ui/scripts/validate-visual-report.mjs"), path],
            capture_output=True, text=True, check=False,
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)


if __name__ == "__main__":
    unittest.main()
