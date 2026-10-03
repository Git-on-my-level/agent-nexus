import sys
import unittest
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from project import github_phase, observation_digest, plan_reads
from readers.agentctl import count_runs, parse_envelope
from readers.fleetctl import parse_status
from readers.github import classify_ci, parse_search
from readers.hermes import job_problems, parse_incidents, parse_jobs, project_job, scrub
from readers.multica import host_label, normalize_issue, parse_issues, signals

NOW = datetime(2026, 10, 4, 12, 0, tzinfo=timezone.utc)
CONFIG = {
    "boards": {
        "agent_work": "board:agent",
        "pull_requests": "board:prs",
        "ops_hygiene": "board:ops",
    },
    "multica": {"connection_id": "multica-main"},
    "github": {"connection_id": "github.com", "owner": "David"},
}


def _read(name, items, **extra):
    read = {
        "name": name, "ok": True, "complete": True, "observed_at": "2026-10-04T12:00:00Z",
        "error": None, "items": items, "present_ids": [item["native_id"] for item in items], "meta": {},
    }
    read.update(extra)
    return read


class MulticaTests(unittest.TestCase):
    def test_done_archive_truncation_is_separate_from_open_board(self):
        page, truncated = parse_issues({"issues": [{"id": "1"}], "total": 457, "has_more": True})
        self.assertEqual(len(page), 1)
        self.assertTrue(truncated)
        _, open_truncated = parse_issues({"issues": [{"id": "1"}], "total": 1, "has_more": False})
        self.assertFalse(open_truncated)

    def test_host_and_signals(self):
        self.assertEqual(host_label("M5 MBP Devin (Fusion)"), "M5 MBP")
        self.assertEqual(signals("reply OK smoke", "SCA-1"), ["reply-ok", "smoke"])
        self.assertEqual(signals("production deploy", "SCA-2"), [])

    def test_phase_and_no_create_when_done(self):
        issue = normalize_issue({
            "id": "11111111-1111-1111-1111-111111111111",
            "identifier": "SCA-1",
            "title": "reply OK the probe",
            "status": "in_review",
            "updated_at": "2026-10-01T00:00:00Z",
            "assignee_id": "agent-1",
        }, {"agent-1": {"name": "M5 MBP Devin (x)", "host": "M5 MBP"}},
            app_url="https://multica.example.invalid", slug="acme", now=NOW)
        plans = plan_reads([_read("multica", [issue])], {}, CONFIG, now=NOW)
        self.assertEqual(plans[0]["facts"]["phase"], "review")
        self.assertTrue(plans[0]["facts"]["in_review_over_72h"])
        self.assertIn("reply-ok", plans[0]["facts"]["signals"])
        self.assertEqual(plans[0]["action"], "create")
        self.assertTrue(plans[0]["url"].endswith("/acme/issues/11111111-1111-1111-1111-111111111111"))

        done = dict(issue, status="done")
        done_plans = plan_reads([_read("multica", [done])], {}, CONFIG, now=NOW)
        self.assertEqual(done_plans[0]["action"], "skip")
        self.assertTrue(done_plans[0]["evidence"][0]["url"])

    def test_failed_read_does_not_close(self):
        known = {("multica", "multica-main", "issue-1"): {"ref": "card:1", "digest": "old", "title": "Old", "owner": "A"}}
        failed = _read("multica", [], ok=False, complete=False, error="timed out")
        self.assertEqual(plan_reads([failed], known, CONFIG, now=NOW), [])

    def test_complete_read_closes_absent_card(self):
        known = {("multica", "multica-main", "missing"): {"ref": "card:1", "digest": None, "title": "Gone", "owner": "A"}}
        plans = plan_reads([_read("multica", [])], known, CONFIG, now=NOW)
        self.assertEqual(plans[0]["facts"]["phase"], "done")
        self.assertIn("absent-as-of", plans[0]["evidence"][0]["ref"])
        self.assertEqual(plans[0]["action"], "observe")


class GitHubTests(unittest.TestCase):
    def test_parse_and_ci(self):
        pulls = parse_search([{
            "repository": {"nameWithOwner": "example/repo"},
            "number": 4, "title": "Fix", "url": "https://github.com/example/repo/pull/4",
            "state": "OPEN", "isDraft": False, "updatedAt": "2026-10-04T00:00:00Z", "createdAt": "2026-10-01T00:00:00Z",
        }])
        self.assertEqual(pulls[0]["native_id"], "example/repo#4")
        self.assertEqual(classify_ci([
            {"conclusion": "SUCCESS", "status": "COMPLETED"},
            {"conclusion": "FAILURE", "status": "COMPLETED"},
        ]), "red")
        self.assertEqual(classify_ci([]), "none")
        self.assertEqual(classify_ci([{"status": "IN_PROGRESS", "conclusion": None}]), "pending")

    def test_phase_rules(self):
        fresh = {"state": "OPEN", "is_draft": False, "ci": "unknown", "updated_at": "2026-10-03T00:00:00Z", "terminal": False}
        self.assertEqual(github_phase(fresh, NOW), "review")
        self.assertEqual(github_phase({**fresh, "is_draft": True}, NOW), "in_progress")
        self.assertEqual(github_phase({**fresh, "ci": "red"}, NOW), "blocked")
        self.assertEqual(github_phase({**fresh, "is_draft": True, "ci": "red"}, NOW), "in_progress")
        stale = {**fresh, "updated_at": "2026-08-01T00:00:00Z"}
        self.assertEqual(github_phase(stale, NOW), "backlog")
        closed = {**fresh, "terminal": True, "state": "MERGED", "url": "https://github.com/example/repo/pull/4"}
        self.assertEqual(github_phase(closed, NOW), "done")
        plans = plan_reads([_read("github", [{
            "native_id": "example/repo#4", "title": "Fix", "url": "https://github.com/example/repo/pull/4",
            "state": "OPEN", "is_draft": False, "ci": "unknown", "updated_at": "2026-08-01T00:00:00Z",
            "terminal": False, "repo": "example/repo", "review_decision": None,
        }])], {}, CONFIG, now=NOW)
        self.assertEqual(plans[0]["facts"]["phase"], "backlog")
        self.assertIn("stale", plans[0]["facts"]["summary"])
        self.assertEqual(plans[0]["owner"], "David")


class HermesTests(unittest.TestCase):
    def test_prompt_is_dropped_and_error_scrubbed(self):
        raw = {
            "id": "job-1", "name": "watchdog", "prompt": "do the secret thing token=abcd",
            "schedule": {"display": "hourly"}, "enabled": True, "state": "paused",
            "paused_at": "2026-10-01T00:00:00Z", "paused_reason": "",
            "last_status": "error", "last_error": "boom Bearer abc.def.ghi and ghp_12345678901234567890",
            "last_run_at": "2026-10-04T00:00:00Z", "next_run_at": "2026-10-05T00:00:00Z",
            "workdir": "/tmp/jobs", "deliver": "local", "script": "echo $API_KEY",
        }
        job = project_job(raw)
        self.assertNotIn("prompt", job)
        self.assertNotIn("script", job)
        self.assertNotIn("secret", job["last_error"])
        self.assertIn("[redacted]", job["last_error"])
        self.assertLessEqual(len(job["last_error"]), 200)
        self.assertEqual(parse_jobs({"jobs": [raw]})[0]["id"], "job-1")
        self.assertEqual(job_problems(job, workdir_missing=True), ["paused-no-reason", "error", "missing-workdir"])
        self.assertEqual(scrub("ok"), "ok")

    def test_incidents_and_cleared_job(self):
        text = """
  inc_1  alerted
    Job:        missing-job
    Type:       script
    Error:      Script exited token=supersecretvalue
"""
        incidents = parse_incidents(text)
        self.assertEqual(incidents[0]["job_id"], "missing-job")
        self.assertNotIn("supersecretvalue", incidents[0]["error"])
        host = {
            "label": "local-host", "ok": True, "complete": True, "jobs": 1, "job_ids": ["job-1"],
            "items": [], "rows": [],
        }
        read = _read("hermes", [], meta={"hosts": [host]})
        known = {("hermes-cron", "local-host", "job-1"): {"ref": "card:1", "digest": None, "title": "watchdog", "owner": "local-host"}}
        plans = plan_reads([read], known, CONFIG, now=NOW)
        self.assertEqual(plans[0]["facts"]["phase"], "done")
        self.assertEqual(plans[0]["reason"], "cleared")

    def test_incomplete_host_does_not_close(self):
        host = {"label": "local-host", "ok": True, "complete": False, "jobs": 1, "job_ids": [], "items": [], "rows": []}
        known = {("hermes-cron", "local-host", "job-1"): {"ref": "card:1", "digest": None, "title": "watchdog", "owner": "local-host"}}
        self.assertEqual(plan_reads([_read("hermes", [], meta={"hosts": [host]})], known, CONFIG, now=NOW), [])


class AgentctlTests(unittest.TestCase):
    def test_counts_and_threshold(self):
        now = NOW
        executions = [
            {"state": "running", "liveness": "alive", "updated_at": "2026-10-04T11:00:00Z"},
            {"state": "running", "liveness": "unreachable", "updated_at": "2026-10-01T00:00:00Z"},
            {"state": "waiting", "liveness": "unknown", "updated_at": "2026-10-04T11:30:00Z"},
        ]
        self.assertEqual(count_runs(executions, now=now, complete=True), {"stuck": 1, "live_running": 1})
        self.assertEqual(count_runs(executions, now=now, complete=False), {"stuck": None, "live_running": None})
        envelope = parse_envelope({"ok": True, "result": {"total": 1, "executions": []}})
        self.assertEqual(envelope["total"], 1)
        host = {
            "label": "remote-host", "ok": True, "complete": True, "over_threshold": True,
            "stuck": 2, "uncollected": 60, "live_running": 1, "version": "v0.11.2", "item": {},
        }
        plans = plan_reads([_read("agentctl", [], meta={"hosts": [host]})], {}, CONFIG, now=NOW)
        self.assertEqual(plans[0]["native_id"], "remote-host/agentctl-hygiene")
        self.assertIn("2 stuck runs, 60 uncollected", plans[0]["title"])
        self.assertEqual(plans[0]["facts"]["phase"], "blocked")

    def test_under_threshold_closes_existing_only(self):
        host = {
            "label": "remote-host", "ok": True, "complete": True, "over_threshold": False,
            "stuck": 0, "uncollected": 3, "live_running": 1,
        }
        plans = plan_reads([_read("agentctl", [], meta={"hosts": [host]})], {}, CONFIG, now=NOW)
        self.assertEqual(plans, [])
        known = {("agentctl", "remote-host", "remote-host/agentctl-hygiene"): {"ref": "card:1", "digest": None, "title": "old", "owner": "remote-host"}}
        closed = plan_reads([_read("agentctl", [], meta={"hosts": [host]})], known, CONFIG, now=NOW)
        self.assertEqual(closed[0]["facts"]["phase"], "done")
        self.assertTrue(closed[0]["evidence"][0]["ref"])


class FleetctlTests(unittest.TestCase):
    def test_root_causes_and_absence(self):
        parsed = parse_status({
            "queues": [
                {"queue": "host-action", "root_cause_count": 1, "root_causes": [
                    {"key": "host/mute", "priority": "high", "hosts": ["remote-host"], "refs": ["remote-host/host/mute"]},
                ]},
                {"queue": "observability", "root_cause_count": 0, "root_causes": []},
            ],
            "inventory": {"hosts": [
                {"host": "remote-host", "state": "mute"},
                {"host": "local-host", "state": "reporting"},
            ]},
        })
        self.assertEqual(parsed["causes"][0]["native_id"], "host/mute")
        self.assertIsNone(parsed["queues"].get("contract"))
        self.assertEqual(parsed["inventory"]["mute"], 1)
        plans = plan_reads([_read("fleetctl", parsed["causes"], meta=parsed)], {}, CONFIG, now=NOW)
        self.assertEqual(plans[0]["authority"], "fleetctl")
        self.assertEqual(plans[0]["connection_id"], "fleet")
        self.assertEqual(plans[0]["action"], "create")
        known = {
            ("fleetctl", "fleet", "host/mute"): {"ref": "card:1", "digest": plans[0]["digest"], "title": plans[0]["title"], "owner": "fleet"},
            ("fleetctl", "fleet", "gone"): {"ref": "card:2", "digest": None, "title": "gone", "owner": "fleet"},
        }
        again = plan_reads([_read("fleetctl", parsed["causes"])], known, CONFIG, now=NOW)
        actions = {plan["native_id"]: plan["action"] for plan in again}
        self.assertEqual(actions["host/mute"], "skip")
        self.assertEqual(actions["gone"], "observe")

    def test_digest_ignores_clock(self):
        item = {"native_id": "host/mute", "priority": "high", "hosts": ["a"], "terminal": False}
        first = plan_reads([_read("fleetctl", [item])], {}, CONFIG, now=NOW)[0]
        second = plan_reads([_read("fleetctl", [item], observed_at="2026-10-05T00:00:00Z")], {}, CONFIG, now=NOW)[0]
        self.assertEqual(first["digest"], second["digest"])
        self.assertEqual(first["digest"], observation_digest(
            "fleetctl\nfleet\nhost/mute", first["facts"]))


if __name__ == "__main__":
    unittest.main()
