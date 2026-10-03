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
        causes = next(item for item in next(panel for panel in report["panels"] if panel["id"] == "overview-fleetctl")["data"]["items"]
                      if item["label"] == "Host-action root causes")
        self.assertEqual(causes["value"], "1")
        self.assertNotIn("Incomplete", causes["detail"])
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
                    {"id": "a", "title": ("abcdefghij " * 7) + "TAILWORD", "created_at": "2026-09-01T00:00:00Z"},
                    {"id": "b", "title": "Newer ask", "created_at": "2026-10-03T00:00:00Z"},
                ],
                "loose_ends": [{
                    "ref": "card:one",
                    "title": "Decide: Close stale non-Omi PRs older than 6 months of quiet",
                    "phase": "blocked",
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
        self.assertIn("2 decisions waiting in your ANX Inbox — oldest: abcdefghij abcdefghij", callout)
        self.assertNotIn("TAILWORD", callout)
        self.assertIn("1 loose end on the decisions board — 1 waiting on Ada (see Loose ends tab/table)", callout)
        self.assertNotIn("Decide: Close", callout)
        self.assertNotIn("6 mon", callout)
        self.assertIn("Prometheus: 2 firing (critical first): FilesystemAlmostFull on hub-a (/)", callout)
        self.assertIn("Paused safety watchdogs: 1 — safety-watchdog on local-host", callout)
        loose = next(panel for panel in report["panels"] if panel["id"] == "loose-ends")
        self.assertEqual(loose["data"]["rows"][0]["cells"][2], "Ada")
        history = record_history([{"at": f"2026-10-03T00:{index:02d}:00Z", "metrics": {"decisions_waiting": index}} for index in range(60)],
                                 at="2026-10-04T12:00:00Z", metrics={"decisions_waiting": 2})
        self.assertEqual(len(history), 50)
        self.assertEqual(history[-1]["at"], "2026-10-04T12:00:00Z")

    def test_repo_chart_is_a_horizontal_top_12(self):
        reads = _reads()
        items = []
        for index in range(13):
            count = 20 - index
            for number in range(count):
                items.append({
                    "native_id": f"org/r{index:02d}#{number + 1}",
                    "repo": f"org/r{index:02d}",
                    "title": "Change",
                    "state": "OPEN", "is_draft": False, "terminal": False,
                    "ci": "green", "updated_at": "2026-10-03T00:00:00Z", "created_at": "2026-09-01T00:00:00Z",
                })
        reads[1]["items"] = items
        report = build_report(reads, generated_at="2026-10-04T12:00:00Z", now=NOW, hosts=[])
        chart = next(panel for panel in report["panels"] if panel["id"] == "prs-by-repo")
        option = chart["data"]["option"]
        self.assertEqual(option["xAxis"]["type"], "value")
        self.assertEqual(option["yAxis"]["type"], "category")
        self.assertTrue(option["yAxis"]["inverse"])
        self.assertEqual(option["yAxis"]["data"][0], "org/r00")
        self.assertEqual(option["series"][0]["data"][0], 20)
        self.assertEqual(option["yAxis"]["data"][-1], "Other (1 repos)")
        self.assertEqual(option["series"][0]["data"][-1], 8)
        self.assertEqual(len(option["yAxis"]["data"]), 13)
        rolled = _reads()
        rolled[1]["items"] = [
            *[_pull(f"org/named-{index:02d}#1", "green", "2026-10-03T00:00:00Z") for index in range(12)],
            *[_pull(f"org/named-{index:02d}#2", "green", "2026-10-03T00:00:00Z") for index in range(12)],
            *[_pull(f"org/tiny-{index:02d}#1", "green", "2026-10-03T00:00:00Z") for index in range(20)],
        ]
        again = build_report(rolled, generated_at="2026-10-04T12:00:00Z", now=NOW, hosts=[])
        again_chart = next(panel for panel in again["panels"] if panel["id"] == "prs-by-repo")["data"]["option"]
        self.assertEqual(again_chart["yAxis"]["data"][0], "Other (20 repos)")
        self.assertEqual(again_chart["series"][0]["data"][0], 20)
        self.assertEqual(again_chart["yAxis"]["data"][1], "org/named-00")

    def test_pr_table_orders_red_then_pending_then_recent_and_notes_stale(self):
        reads = _reads()
        items = [
            _pull("example/repo#1", "red", "2026-08-01T00:00:00Z"),
            _pull("example/repo#2", "red", "2026-10-04T00:00:00Z"),
            _pull("example/repo#3", "pending", "2026-10-03T00:00:00Z"),
            _pull("example/repo#4", "pending", "2026-07-01T00:00:00Z"),
            _pull("example/repo#5", "green", "2026-10-02T00:00:00Z"),
            _pull("example/repo#6", "red", "2026-10-04T01:00:00Z", draft=True),
        ]
        items.extend(_pull(f"example/old#{index:02d}", "green", "2024-06-01T00:00:00Z") for index in range(1, 31))
        reads[1]["items"] = items
        reads[1]["meta"] = {"ci_checked": 4}
        report = build_report(reads, generated_at="2026-10-04T12:00:00Z", now=NOW, hosts=[])
        table = next(panel for panel in report["panels"] if panel["id"] == "pr-ci")
        self.assertIn("CI checked for 4 recently updated PRs", table["title"])
        identities = [row["cells"][1] for row in table["data"]["rows"]]
        self.assertEqual(identities[:5], [
            "example/repo#2", "example/repo#1", "example/repo#3", "example/repo#4", "example/repo#5",
        ])
        self.assertNotIn("example/repo#6", identities)
        self.assertEqual(len(table["data"]["rows"]), 31)
        self.assertEqual(
            table["data"]["rows"][-1]["cells"][2],
            "5 more stale PRs (no update in 30 days) — on board Fleet · Pull requests, Backlog",
        )

    def test_cron_problems_sort_error_then_missing_workdir_then_paused(self):
        reads = _reads()
        reads[2] = {
            "name": "hermes", "ok": True, "complete": True, "observed_at": "2026-10-04T11:06:00Z",
            "items": [], "meta": {"hosts": [
                {"label": "host-b", "ok": True, "complete": True, "rows": [
                    {"name": "paused-job", "problems": ["paused-no-reason"], "last_status": "ok", "schedule": "hourly"},
                    {"name": "err-b", "problems": ["error"], "last_status": "error", "schedule": "hourly"},
                ]},
                {"label": "host-a", "ok": True, "complete": True, "rows": [
                    {"name": "missing", "problems": ["missing-workdir"], "last_status": "error", "schedule": "daily"},
                    {"name": "err-a", "problems": ["error", "missing-workdir"], "last_status": "error", "schedule": "daily"},
                ]},
            ]},
        }
        report = build_report(reads, generated_at="2026-10-04T12:00:00Z", now=NOW, hosts=[])
        table = next(panel for panel in report["panels"] if panel["id"] == "cron-problems")
        self.assertEqual(
            [(row["cells"][0], row["cells"][1]) for row in table["data"]["rows"]],
            [("host-a", "err-a"), ("host-b", "err-b"), ("host-a", "missing"), ("host-b", "paused-job")],
        )

    def test_incomplete_headline_carries_a_partial_marker(self):
        reads = _reads()
        reads[1]["complete"] = False
        fleet = reads[4]
        fleet["complete"] = False
        fleet["items"] = [{"native_id": f"cause-{index}"} for index in range(46)]
        fleet["meta"] = {**fleet["meta"], "withheld": "1 untrusted reports"}
        report = build_report(reads, generated_at="2026-10-04T12:00:00Z", now=NOW, hosts=[])
        fleet_strip = next(panel for panel in report["panels"] if panel["id"] == "overview-fleetctl")
        causes = next(item for item in fleet_strip["data"]["items"] if item["label"] == "Host-action root causes")
        self.assertEqual(causes["value"], "46+")
        self.assertIn("Incomplete read", causes["detail"])
        self.assertIn("lower bound", causes["detail"])
        reporting = next(item for item in fleet_strip["data"]["items"] if item["label"] == "Reporting")
        self.assertEqual(reporting["value"], "1+")
        now = next(panel for panel in report["panels"] if panel["id"] == "overview-now")
        pulls = next(item for item in now["data"]["items"] if item["label"] == "Open pull requests")
        self.assertEqual(pulls["value"], "1+")
        self.assertIn("Incomplete read", pulls["detail"])
        queues = next(panel for panel in report["panels"] if panel["id"] == "fleetctl-queues")
        self.assertIn("Incomplete", queues["data"]["caption"])


def _pull(native, ci, updated, draft=False):
    repo, number = native.split("#")
    return {
        "native_id": native, "repo": repo, "title": "Change",
        "url": f"https://github.com/{repo}/pull/{number}",
        "state": "OPEN", "is_draft": draft, "terminal": False, "ci": ci,
        "updated_at": updated, "created_at": "2024-01-01T00:00:00Z",
    }


if __name__ == "__main__":
    unittest.main()
