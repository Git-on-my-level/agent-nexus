import json
import shlex
import sys
import unittest
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from project import github_phase, observation_digest, operator_name, plan_reads
from readers.agentctl import count_runs, parse_envelope
from readers.fleetctl import parse_status, read_fleetctl, withheld_reason
from readers.github import classify_ci, confirm_disappeared, github_pr_url, parse_search, read_github
from readers.hermes import _DIR_SCRIPT, job_problems, parse_incidents, parse_jobs, project_job, read_hermes, scrub
from readers.multica import host_label, issue_url, normalize_issue, parse_issues, signals
from readers.nexus import normalize_inbox, normalize_loose, on_board
from readers.prometheus import alert_native_id, alert_title, host_from_instance, parse_alerts, read_prometheus, valid_alerts_url
from readers.run import Budget, BudgetRunner, HostExec, RunResult, ssh_argv, valid_remote_config

NOW = datetime(2026, 10, 4, 12, 0, tzinfo=timezone.utc)
CONFIG = {
    "boards": {
        "agent_work": "board:agent",
        "pull_requests": "board:prs",
        "ops_hygiene": "board:ops",
    },
    "multica": {"connection_id": "multica-main"},
    "github": {"connection_id": "github.com", "owner": "Example Owner"},
    "operator_name": "Ada",
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
        self.assertEqual(host_label("Example Host Devin (Fusion)"), "Example Host")
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
        }, {"agent-1": {"name": "Example Agent (x)", "host": "Example Agent"}},
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
        self.assertEqual(plans[0]["owner"], "Example Owner")
        nameless = {**CONFIG, "github": {"connection_id": "github.com"}}
        again = plan_reads([_read("github", [{
            "native_id": "example/repo#4", "title": "Fix", "url": "https://github.com/example/repo/pull/4",
            "state": "OPEN", "is_draft": False, "ci": "unknown", "updated_at": "2026-10-03T00:00:00Z",
            "terminal": False, "repo": "example/repo",
        }])], {}, nameless, now=NOW)
        self.assertEqual(again[0]["owner"], "Ada")
        self.assertEqual(operator_name({}), "Operator")

    def test_search_miss_does_not_close_until_view_confirms(self):
        known = {("github", "github.com", "example/repo#9"): {"ref": "card:1", "digest": None, "title": "old", "owner": "Example Owner"}}
        self.assertEqual(plan_reads([_read("github", [])], known, CONFIG, now=NOW), [])
        confirmed = _read("github", [], meta={"confirmed_closed": ["example/repo#9"]})
        plans = plan_reads([confirmed], known, CONFIG, now=NOW)
        self.assertEqual(plans[0]["facts"]["phase"], "done")
        self.assertEqual(plans[0]["reason"], "absent")
        self.assertIsNone(github_pr_url("not a pr"))
        self.assertEqual(github_pr_url("example/repo#9"), "https://github.com/example/repo/pull/9")

        def handler(argv, timeout):
            url = argv[argv.index("view") + 1]
            state = "MERGED" if url.endswith("/9") else "OPEN"
            if url.endswith("/8"):
                return RunResult(False, argv, 1, "", "", "timed out")
            return RunResult(True, argv, 0, json.dumps({"state": state}), "", None)

        read = _read("github", [{"native_id": "example/repo#4", "title": "stay"}])
        cards = {
            ("github", "github.com", "example/repo#4"): {"ref": "card:4"},
            ("github", "github.com", "example/repo#8"): {"ref": "card:8"},
            ("github", "github.com", "example/repo#9"): {"ref": "card:9"},
            ("multica", "multica-main", "issue-1"): {"ref": "card:m"},
        }
        confirm_disappeared(_Runner(handler), read, cards, connection_id="github.com")
        self.assertEqual(read["meta"]["confirmed_closed"], ["example/repo#9"])
        self.assertEqual(read["meta"]["absence_checked"], 2)

    def test_closure_confirmation_skips_done_and_prefers_recent_open(self):
        calls = []

        def handler(argv, timeout):
            calls.append(argv)
            return RunResult(True, argv, 0, json.dumps({"state": "CLOSED"}), "", None)

        read = _read("github", [{"native_id": "example/repo#99", "title": "stay"}])
        cards = {
            ("github", "github.com", "example/repo#99"): {
                "ref": "card:99", "phase": "review", "seen_at": "2026-10-04T00:00:00Z",
            },
            ("github", "github.com", "aaa/cancel#1"): {"ref": "card:c", "phase": "cancelled"},
        }
        for index in range(20):
            cards[("github", "github.com", f"aaa/done#{index + 1}")] = {"ref": f"done:{index}", "phase": "done"}
        for index in range(21):
            cards[("github", "github.com", f"example/repo#{index + 1}")] = {
                "ref": f"card:{index}", "phase": "review",
                "seen_at": f"2026-08-{index + 1:02d}T00:00:00Z",
            }
        confirm_disappeared(_Runner(handler), read, cards, connection_id="github.com")
        viewed = [argv[argv.index("view") + 1] for argv in calls]
        self.assertEqual(viewed[0], "https://github.com/example/repo/pull/21")
        self.assertEqual(len(viewed), 20)
        self.assertNotIn("https://github.com/example/repo/pull/1", viewed)
        self.assertNotIn("https://github.com/example/repo/pull/99", viewed)
        self.assertFalse(any("aaa/" in url for url in viewed))
        self.assertEqual(read["meta"]["confirmed_closed"][0], "example/repo#21")
        self.assertEqual(read["meta"]["absence_checked"], 20)

    def test_remote_config_rejects_a_leading_dash(self):
        self.assertFalse(valid_remote_config("-rf"))
        self.assertFalse(valid_remote_config("--contract"))
        self.assertFalse(valid_remote_config(""))
        self.assertTrue(valid_remote_config("/opt/fleetctl/contract"))
        self.assertTrue(valid_remote_config("fleet-ctl"))
        self.assertTrue(valid_remote_config("$HOME/.fleetctl/reports"))
        self.assertTrue(valid_remote_config("~/not-expanded"))

    def test_ci_checked_counts_recent_pulls_only(self):
        def handler(argv, timeout):
            if "search" in argv and "open" in argv:
                return RunResult(True, argv, 0, json.dumps([{
                    "repository": {"nameWithOwner": "example/repo"},
                    "number": 4, "title": "Fix", "url": "https://github.com/example/repo/pull/4",
                    "state": "OPEN", "isDraft": False,
                    "updatedAt": "2026-10-04T00:00:00Z", "createdAt": "2026-10-01T00:00:00Z",
                }, {
                    "repository": {"nameWithOwner": "example/repo"},
                    "number": 5, "title": "Old", "url": "https://github.com/example/repo/pull/5",
                    "state": "OPEN", "isDraft": False,
                    "updatedAt": "2026-08-01T00:00:00Z", "createdAt": "2026-07-01T00:00:00Z",
                }]), "", None)
            if "search" in argv:
                return RunResult(True, argv, 0, "[]", "", None)
            if "view" in argv:
                return RunResult(True, argv, 0, json.dumps({"statusCheckRollup": [{"conclusion": "FAILURE", "status": "COMPLETED"}]}), "", None)
            return RunResult(False, argv, 1, "", "", "unexpected")

        read = read_github(HostExec(_Runner(handler)), {}, now=NOW)
        self.assertEqual(read["meta"]["ci_checked"], 1)
        self.assertEqual(read["items"][0]["ci"], "red")
        self.assertEqual(read["items"][1]["ci"], "unknown")


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

    def test_unrecognized_incidents_are_not_an_empty_list(self):
        self.assertEqual(parse_incidents(""), [])
        self.assertEqual(parse_incidents("No cron failure incidents recorded.\n  (filtered by state 'alerted')\n"), [])
        self.assertEqual(parse_incidents("\x1b[2mNo cron failure incidents recorded.\x1b[0m\n"), [])
        with self.assertRaises(ValueError):
            parse_incidents("usage: hermes cron incidents\nunexpected table\n")

    def test_partial_host_marks_the_read_incomplete_and_does_not_close(self):
        jobs = {"jobs": [{
            "id": "job-1", "name": "watchdog", "enabled": True, "state": "ok",
            "last_status": "ok", "workdir": "/tmp/example",
        }]}

        def handler(argv, timeout):
            if argv[:1] == ["cat"]:
                return RunResult(True, argv, 0, json.dumps(jobs), "", None)
            if argv[:3] == ["hermes", "cron", "incidents"]:
                return RunResult(True, argv, 0, "not the incident format\n", "", None)
            if argv[:2] == ["sh", "-c"]:
                return RunResult(False, argv, 1, "", "", "syntax error near do")
            return RunResult(False, argv, 1, "", "", "unexpected")

        read = read_hermes(HostExec(_Runner(handler)), {"hosts": [{"label": "host-a", "local": True}]}, now=NOW)
        self.assertTrue(read["ok"], read.get("error"))
        self.assertFalse(read["complete"])
        self.assertEqual(read["meta"]["unchecked_hosts"], ["host-a"])
        self.assertFalse(read["meta"]["hosts"][0]["complete"])
        known = {("hermes-cron", "host-a", "job-1"): {"ref": "card:1", "digest": None, "title": "watchdog", "owner": "host-a"}}
        self.assertEqual(plan_reads([read], known, CONFIG, now=NOW), [])

    def test_remote_workdir_script_is_one_quoted_argument(self):
        argv = ssh_argv("host-a", ["sh", "-c", _DIR_SCRIPT])
        parts = shlex.split(argv[-1])
        self.assertEqual(parts[4:7], ["sh", "-c", _DIR_SCRIPT])
        self.assertIn('PATH="$HOME/.local/bin:', parts[2])
        home = ssh_argv("hub-1", ["cat", "$HOME/.hermes/cron/jobs.json"])
        self.assertIn('"$HOME/.hermes/cron/jobs.json"', home[-1])
        with self.assertRaises(ValueError):
            ssh_argv("-oProxyCommand=id", ["true"])

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
        blocked = _read("fleetctl", parsed["causes"], complete=False, meta={**parsed, "withheld": "1 untrusted reports"})
        self.assertEqual(
            [plan["native_id"] for plan in plan_reads([blocked], known, CONFIG, now=NOW)],
            ["host/mute"],
        )

    def test_untrusted_or_caveat_blocks_closure(self):
        self.assertIsNone(withheld_reason({"queues": [{"queue": "host-action", "caveat": ""}], "inventory": {"untrusted": 0}}))
        self.assertEqual(withheld_reason({"inventory": {"untrusted": 2}}), "2 untrusted reports")
        self.assertEqual(withheld_reason({"trust": {"untrusted": 1}}), "1 untrusted reports")
        caveat = withheld_reason({"queues": [{"queue": "host-action", "caveat": "1 withheld as untrusted"}]})
        self.assertIn("withheld", caveat)
        # A mute host is trusted:false without being an untrusted report.
        self.assertIsNone(withheld_reason({"inventory": {"untrusted": 0, "hosts": [{"host": "host-a", "trusted": False}]}}))

    def test_digest_ignores_clock(self):
        item = {"native_id": "host/mute", "priority": "high", "hosts": ["a"], "terminal": False}
        first = plan_reads([_read("fleetctl", [item])], {}, CONFIG, now=NOW)[0]
        second = plan_reads([_read("fleetctl", [item], observed_at="2026-10-05T00:00:00Z")], {}, CONFIG, now=NOW)[0]
        self.assertEqual(first["digest"], second["digest"])
        self.assertEqual(first["digest"], observation_digest(
            "fleetctl\nfleet\nhost/mute", first["facts"]))

    def test_other_inventory_states_stay_visible(self):
        parsed = parse_status({"queues": [], "inventory": {"hosts": [
            {"host": "a", "state": "reporting"},
            {"host": "b", "state": "observed_only"},
            {"host": "c", "state": "retired"},
        ]}})
        self.assertEqual(parsed["inventory"]["reporting"], 1)
        self.assertEqual(parsed["other_states"], {"observed_only": 1, "retired": 1})

    def test_status_can_target_the_ssh_hub(self):
        calls = []

        def handler(argv, timeout):
            calls.append((argv, timeout))
            if _ssh_command(argv) == ["/usr/bin/true"]:
                return RunResult(True, argv, 0, "", "", None)
            self.assertLessEqual(timeout, 40)
            return RunResult(True, argv, 0, json.dumps({
                "queues": [{"queue": "host-action", "root_cause_count": 0, "root_causes": [], "caveat": "2 withheld as untrusted"}],
                "inventory": {"hosts": [], "untrusted": 2},
                "trust": {"untrusted": 2},
            }), "", None)

        read = read_fleetctl(HostExec(_Runner(handler)), {
            "fleetctl": {
                "binary": "/usr/local/bin/fleetctl",
                "ssh": "hub-1",
                "contract_dir": "/opt/fleetctl/contract",
                "reports_dir": "$HOME/.fleetctl/reports",
            },
        }, now=NOW)
        self.assertTrue(read["ok"])
        self.assertFalse(read["complete"])
        self.assertIn("untrusted", read["meta"]["withheld"])
        remote = [argv for argv, _ in calls if "status" in argv[-1]][0]
        self.assertEqual(remote[:3], ["ssh", "-o", "BatchMode=yes"])
        self.assertIn("hub-1", remote)
        self.assertIn("/usr/local/bin/fleetctl", remote[-1])
        self.assertIn('"$HOME/.fleetctl/reports"', remote[-1])
        rejected = read_fleetctl(HostExec(_Runner(handler)), {
            "fleetctl": {
                "binary": "/usr/local/bin/fleetctl",
                "ssh": "-oProxyCommand=id",
                "contract_dir": "/opt/fleetctl/contract",
                "reports_dir": "$HOME/.fleetctl/reports;touch /tmp/x",
            },
        }, now=NOW)
        self.assertFalse(rejected["ok"])


class PrometheusTests(unittest.TestCase):
    def test_firing_identity_and_pending_is_not_a_card(self):
        labels = {
            "alertname": "FilesystemAlmostFull",
            "device": "/dev/sda1",
            "instance": "hub-a.example.ts.net:9100",
            "mountpoint": "/",
            "severity": "warning",
        }
        self.assertEqual(
            host_from_instance(labels["instance"], ".example.ts.net"),
            "hub-a",
        )
        self.assertEqual(alert_title("FilesystemAlmostFull", "hub-a", labels),
                         "FilesystemAlmostFull on hub-a (/)")
        first = alert_native_id("FilesystemAlmostFull", labels)
        self.assertEqual(first, alert_native_id("FilesystemAlmostFull", dict(reversed(list(labels.items())))))
        parsed = parse_alerts({
            "status": "success",
            "data": {"alerts": [
                {"state": "firing", "labels": labels},
                {"state": "pending", "labels": {**labels, "mountpoint": "/var"}},
                {"state": "firing", "labels": {**labels, "alertname": "MacMemoryPressure", "instance": "hub-b.example.ts.net:9100", "severity": "critical"}},
            ]},
        }, suffix=".example.ts.net")
        self.assertEqual(parsed["pending"], 1)
        self.assertEqual([item["severity"] for item in parsed["firing"]], ["critical", "warning"])
        config = {**CONFIG, "prometheus": {"connection_id": "hub-1"}}
        plans = plan_reads([_read("prometheus", parsed["firing"], meta={"pending": 1})], {}, config, now=NOW)
        self.assertEqual(len(plans), 2)
        self.assertEqual(plans[0]["authority"], "prometheus")
        self.assertEqual(plans[0]["connection_id"], "hub-1")
        self.assertEqual(plans[0]["facts"]["phase"], "blocked")
        known = {("prometheus", "hub-1", item["native_id"]): {"ref": "card:1", "digest": None, "title": "old", "owner": "h"}
                 for item in parsed["firing"]}
        known[("prometheus", "hub-1", "gone")] = {"ref": "card:9", "digest": None, "title": "gone", "owner": "h"}
        closed = plan_reads([_read("prometheus", [], meta={"pending": 1})], known, config, now=NOW)
        self.assertEqual({plan["native_id"] for plan in closed}, {item["native_id"] for item in parsed["firing"]} | {"gone"})
        self.assertTrue(all(plan["facts"]["phase"] == "done" and plan["evidence"][0]["ref"] for plan in closed))
        self.assertEqual(plan_reads([_read("prometheus", [], ok=False, complete=False)], known, config, now=NOW), [])

    def test_curl_uses_ssh_and_a_hard_timeout(self):
        calls = []

        def handler(argv, timeout):
            calls.append((argv, timeout))
            if _ssh_command(argv) == ["/usr/bin/true"]:
                return RunResult(True, argv, 0, "", "", None)
            return RunResult(True, argv, 0, json.dumps({"status": "success", "data": {"alerts": []}}), "", None)

        read = read_prometheus(HostExec(_Runner(handler)), {
            "prometheus": {"ssh": "hub-1", "alerts_url": "http://127.0.0.1:9090/api/v1/alerts",
                           "instance_suffix": ".example.ts.net"},
        }, now=NOW)
        self.assertTrue(read["ok"])
        self.assertEqual(read["meta"]["pending"], 0)
        self.assertEqual(read["items"], [])
        remote = [argv for argv, timeout in calls if "curl" in argv[-1]]
        self.assertEqual(_ssh_command(remote[0]), ["curl", "-s", "-m", "10", "http://127.0.0.1:9090/api/v1/alerts"])
        quoted = ssh_argv("hub-1", ["curl", "http://127.0.0.1:9090/api/v1/alerts?x=1&y=2"])
        self.assertIn("'http://127.0.0.1:9090/api/v1/alerts?x=1&y=2'", quoted[-1])
        self.assertFalse(valid_alerts_url("http://127.0.0.1:9090/api/v1/alerts?x=1&y=2"))
        self.assertFalse(valid_alerts_url("http://example.test/alerts"))
        self.assertLessEqual(calls[-1][1], 25)
        injected = read_prometheus(HostExec(_Runner(handler)), {
            "prometheus": {"ssh": "hub-1", "alerts_url": "http://169.254.169.254/latest/meta-data"},
        }, now=NOW)
        self.assertFalse(injected["ok"])
        failed = read_prometheus(HostExec(_Runner(lambda argv, timeout: RunResult(False, argv, 1, "", "", "timed out"))), {
            "prometheus": {"ssh": "hub-1"},
        }, now=NOW)
        self.assertFalse(failed["ok"])
        self.assertIsNone(failed["meta"]["firing"])


class NexusReadTests(unittest.TestCase):
    def test_inbox_and_loose_ends_are_not_projected_as_new_cards(self):
        self.assertEqual(issue_url("https://multica.example.invalid", "example-workspace", "abc"),
                         "https://multica.example.invalid/example-workspace/issues/abc")
        inbox = normalize_inbox({"id": "in-1", "title": "Ratify the rulings", "kind": "ask", "created_at": "2026-10-01T00:00:00Z"})
        loose = normalize_loose({
            "ref": "card:one", "title": "Decide: Ratify the rulings", "phase": "blocked",
            "next_actor": "Ada", "next_action": "Answer the inbox item", "board_ref": "board:example-loose-ends",
        })
        self.assertTrue(on_board({"board_ref": "board:example-loose-ends"}, "board:example-loose-ends"))
        self.assertIsNone(normalize_loose({"title": "Finished", "phase": "done", "next_actor": "Ada"}))
        read = _read("nexus", [], meta={"inbox_ok": True, "board_ok": True, "inbox": [inbox], "loose_ends": [loose]})
        self.assertEqual(plan_reads([read], {}, CONFIG, now=NOW), [])

    def test_reader_only_lists_inbox_and_nexus_work(self):
        calls = []

        def handler(argv, timeout):
            calls.append(argv)
            if "inbox" in argv:
                return RunResult(True, argv, 0, json.dumps({"ok": True, "result": {"items": [
                    {"id": "in-1", "title": "Ask", "kind": "ask", "created_at": "2026-10-01T00:00:00Z"},
                ]}}), "", None)
            if "list" in argv and "work" in argv:
                return RunResult(True, argv, 0, json.dumps({"ok": True, "result": {"work": [{
                    "ref": "card:one", "title": "Loose", "phase": "ready", "board_ref": "board:example",
                    "next_actor": "Ada", "next_action": "Decide",
                }]}}), "", None)
            return RunResult(False, argv, 1, "", "", "unexpected command")

        from readers.nexus import read_nexus
        read = read_nexus(HostExec(_Runner(handler)), {
            "base_url": "https://anx.example.invalid/ws/example",
            "agent": "fleet-sync",
            "anx_binary": "anx",
            "nexus": {"loose_ends_board": "board:example"},
        }, now=NOW)
        self.assertTrue(read["ok"], read.get("error"))
        self.assertEqual(read["meta"]["inbox"][0]["title"], "Ask")
        self.assertEqual(read["items"][0]["next_actor"], "Ada")
        text = [" ".join(argv) for argv in calls]
        self.assertTrue(any("debug inbox list" in item for item in text))
        self.assertTrue(any("work list" in item and "--source nexus" in item for item in text))
        self.assertFalse(any("respond" in item or " create" in item or " move" in item for item in text))


class BudgetTests(unittest.TestCase):
    def test_exhausted_budget_fails_the_command(self):
        clock = {"now": 0}
        budget = Budget(5, now=lambda: clock["now"])
        calls = []

        def inner(argv, *, timeout, input_text=None):
            calls.append(timeout)
            return RunResult(True, argv, 0, "", "", None)

        runner = BudgetRunner(inner, budget)
        self.assertTrue(runner(["true"], timeout=40).ok)
        self.assertEqual(calls, [5])
        clock["now"] = 10
        missed = runner(["true"], timeout=40)
        self.assertFalse(missed.ok)
        self.assertIn("budget", missed.error)


def _ssh_command(argv: list[str]) -> list[str]:
    if not argv or argv[0] != "ssh":
        return list(argv)
    return shlex.split(argv[-1])[4:]


class _Runner:
    def __init__(self, handler):
        self.handler = handler

    def __call__(self, argv, *, timeout, input_text=None):
        return self.handler(argv, timeout)


if __name__ == "__main__":
    unittest.main()
