import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from report import build_report, changed_besides_generated_at, headline_snapshot, record_history

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
            "meta": {"ci_checked": 1},
        },
        {
            "name": "hermes", "ok": False, "complete": False, "observed_at": "2026-10-04T11:06:00Z",
            "error": "remote-host: ssh remote-host failed: token=sekritvalue", "items": [],
            "meta": {"hosts": [{
                "label": "remote-host", "ok": False, "complete": False,
                "error": "ssh remote-host failed: token=sekritvalue",
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
        self.assertIn("[redacted]", unavailable["data"]["text"])
        self.assertNotIn("sekritvalue", unavailable["data"]["text"])
        cron_chart = next(panel for panel in report["panels"] if panel["id"] == "cron-health")
        self.assertIn("Not fully checked: remote-host", cron_chart["data"]["caption"])
        self.assertIn("remote-host (partial)", cron_chart["data"]["option"]["xAxis"]["data"])
        prs = next(panel for panel in report["panels"] if panel["id"] == "pr-ci")
        self.assertIn("CI checked for 1 recently updated PRs", prs["title"])
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

    def test_brief_is_short_and_failed_reads_are_not_zero(self):
        report = build_report(
            _reads(), generated_at="2026-10-04T12:00:00Z", now=NOW,
            hosts=[{"label": "local-host", "local": True}],
        )
        callout = next(panel for panel in report["panels"] if panel["id"] == "needs-operator")
        lines = callout["data"]["text"].splitlines()
        self.assertLessEqual(len(lines), 8)
        self.assertTrue(any("older than 72h" in line and "triage, don't read each" in line for line in lines))
        self.assertTrue(any("red CI" in line and "CI checked for 1 recently updated PRs" in line for line in lines))
        self.assertNotIn("Prometheus: 0", callout["data"]["text"])
        self.assertIn("Unavailable:", callout["data"]["text"])
        strip = next(panel for panel in report["panels"] if panel["id"] == "overview-now")
        values = {item["label"]: item["value"] for item in strip["data"]["items"]}
        self.assertEqual(values["Decisions for Operator"], "unknown")
        self.assertEqual(values["Aging reviews"], "1")
        self.assertNotIn("trend", strip["data"]["items"][0])
        firing = next(panel for panel in report["panels"] if panel["id"] == "overview-fleetctl")
        firing_item = next(item for item in firing["data"]["items"] if item["label"] == "Firing alerts")
        self.assertEqual(firing_item["value"], "unknown")
        alerts = next(panel for panel in report["panels"] if panel["id"] == "prometheus-alerts")
        self.assertIn("not zero alerts", alerts["data"]["caption"])

    def test_decision_count_excludes_aging_reviews_and_trends_need_two_runs(self):
        reads = _reads()
        reads[2] = {
            "name": "hermes", "ok": True, "complete": True, "observed_at": "2026-10-04T11:06:00Z",
            "items": [], "meta": {"hosts": [{
                "label": "local-host", "ok": True, "complete": True, "rows": [
                    {"id": "job-1", "name": "safety-watchdog", "watchdog": True, "paused": True, "problems": ["paused-no-reason"]},
                ],
            }]},
        }
        reads.append({
            "name": "prometheus", "ok": True, "complete": True, "observed_at": "2026-10-04T11:09:00Z",
            "items": [
                {"native_id": "FilesystemAlmostFull/abc", "title": "FilesystemAlmostFull on hub-a (/)",
                 "severity": "critical", "host": "hub-a"},
                {"native_id": "MacMemoryPressure/def", "title": "MacMemoryPressure on hub-b",
                 "severity": "warning", "host": "hub-b"},
            ],
            "meta": {"pending": 3, "firing": 2},
        })
        reads.append({
            "name": "nexus", "ok": True, "complete": True, "observed_at": "2026-10-04T11:10:00Z",
            "items": [{
                "ref": "card:one", "title": "Decide the thing", "phase": "blocked",
                "next_actor": "Ada", "next_action": "Answer it",
            }],
            "meta": {
                "inbox_ok": True, "board_ok": True,
                "inbox": [
                    {"id": "a", "title": "Older ask", "created_at": "2026-09-01T00:00:00Z"},
                    {"id": "b", "title": "Newer ask", "created_at": "2026-10-03T00:00:00Z"},
                ],
                "loose_ends": [{
                    "ref": "card:one", "title": "Decide the thing", "phase": "blocked",
                    "next_actor": "Ada", "next_action": "Answer it",
                }],
            },
        })
        by_name = {read["name"]: read for read in reads}
        snapshot = headline_snapshot(by_name, NOW)
        self.assertEqual(snapshot["decisions_waiting"], 2)
        self.assertEqual(snapshot["aging_reviews"], 1)
        # Inbox 2 + red CI 1 + requested changes 0 (the red PR is not counted twice) + 1 watchdog.
        self.assertEqual(snapshot["decisions_for_operator"], 4)
        self.assertEqual(snapshot["prometheus_firing"], 2)
        self.assertEqual(snapshot["prometheus_pending"], 3)
        history = record_history([], at="2026-10-04T11:00:00Z", metrics=snapshot)
        report = build_report(reads, generated_at="2026-10-04T12:00:00Z", now=NOW, hosts=[], history=history, operator="Ada")
        item = next(panel for panel in report["panels"] if panel["id"] == "overview-now")["data"]["items"][0]
        self.assertNotIn("trend", item)
        history = record_history(history, at="2026-10-04T12:00:00Z", metrics=snapshot)
        self.assertEqual(len(history), 2)
        report = build_report(reads, generated_at="2026-10-04T12:00:00Z", now=NOW, hosts=[], history=history, operator="Ada")
        item = next(item for item in next(panel for panel in report["panels"] if panel["id"] == "overview-now")["data"]["items"]
                    if item["label"] == "Decisions for Ada")
        self.assertEqual(item["trend"], [4, 4])
        self.assertIn("2026-10-04T11:00:00Z", item["trend_label"])
        callout = next(panel for panel in report["panels"] if panel["id"] == "needs-operator")["data"]["text"]
        self.assertLessEqual(len(callout.splitlines()), 8)
        self.assertIn("2 decisions waiting in your ANX Inbox — oldest: Older ask", callout)
        self.assertIn("1 loose ends on the decisions board — 1 next actor Ada", callout)
        self.assertIn("Prometheus: 2 firing (critical first): FilesystemAlmostFull on hub-a (/)", callout)
        self.assertIn("Paused safety watchdogs: 1 — safety-watchdog on local-host", callout)
        loose = next(panel for panel in report["panels"] if panel["id"] == "loose-ends")
        self.assertEqual(loose["data"]["rows"][0]["cells"][2], "Ada")
        history = record_history([{"at": f"2026-10-03T00:{index:02d}:00Z", "metrics": {"decisions_waiting": index}} for index in range(60)],
                                 at="2026-10-04T12:00:00Z", metrics={"decisions_waiting": 2})
        self.assertEqual(len(history), 50)
        self.assertEqual(history[-1]["at"], "2026-10-04T12:00:00Z")


if __name__ == "__main__":
    unittest.main()
