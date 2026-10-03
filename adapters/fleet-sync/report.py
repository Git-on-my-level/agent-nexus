"""Build one anx.visual-report from source reads. Unknown values stay null."""

from __future__ import annotations

import json
import re
from datetime import datetime, timezone
from urllib.parse import urlsplit

from project import MULTICA_PHASE, github_phase

OPEN_PHASES = ("backlog", "ready", "in_progress", "blocked", "review")
_ID = re.compile(r"[^A-Za-z0-9._:-]+")


def build_report(reads: list[dict], *, generated_at: str, now: datetime, hosts: list[dict],
                 history: list[dict] | None = None) -> dict:
    by_name = {read["name"]: read for read in reads}
    sources: list[dict] = []
    source_ids: set[str] = set()

    def add_source(raw_id: str, label: str, url: str | None, observed_at: str | None, kind: str) -> str | None:
        safe = _http(url)
        if not safe or len(sources) >= 60:
            return None
        sid = _ident(raw_id)
        if sid in source_ids:
            return sid
        sources.append({
            "id": sid,
            "label": label[:200],
            "url": safe,
            "observed_at": observed_at,
            "kind": kind[:80],
        })
        source_ids.add(sid)
        return sid

    multica = by_name.get("multica")
    github = by_name.get("github")
    hermes = by_name.get("hermes")
    agentctl = by_name.get("agentctl")
    fleet = by_name.get("fleetctl")
    prometheus = by_name.get("prometheus")
    nexus = by_name.get("nexus")
    history = history or []
    multica_source = None
    github_source = None
    if multica and multica.get("ok"):
        multica_source = add_source(
            "multica", "Multica", (multica.get("meta") or {}).get("workspace_url"),
            multica.get("observed_at"), "issue-tracker",
        )
    if github and github.get("ok"):
        github_source = add_source(
            "github", "GitHub pull requests",
            "https://github.com/pulls?q=is%3Aopen+is%3Apr+author%3A%40me",
            github.get("observed_at"), "pull-request",
        )

    panels = []
    panels.extend(_unavailable_panels(reads))
    overview_ids = [panel["id"] for panel in panels]
    cited = [sid for sid in (multica_source, github_source) if sid]
    strips, strip_ids = _strips(by_name, now, cited, history)
    panels.extend(strips)
    overview_ids.extend(strip_ids)
    callout = _callout(by_name, now)
    panels.append(callout)
    overview_ids.append(callout["id"])
    loose = _loose_table(nexus)
    panels.append(loose)
    overview_ids.append(loose["id"])
    phase = _phase_chart(by_name, now, cited)
    panels.append(phase)
    overview_ids.append(phase["id"])

    host_chart = _multica_host_chart(multica)
    review_table = _review_table(multica, now, add_source, multica_source)
    panels.extend([host_chart, review_table])

    repo_chart = _repo_chart(github)
    age_chart = _age_chart(github, now)
    pr_table = _pr_table(github, now, add_source, github_source)
    panels.extend([repo_chart, age_chart, pr_table])

    cron_chart = _cron_chart(hermes)
    problem_table = _problem_table(hermes)
    runs_chart = _runs_chart(agentctl)
    queue_chart = _queue_chart(fleet)
    alert_chart = _alert_chart(prometheus)
    panels.extend([cron_chart, problem_table, runs_chart, queue_chart, alert_chart])

    host_table = _host_table(hosts, hermes, agentctl, fleet)
    panels.append(host_table)

    report = {
        "kind": "anx.visual-report",
        "schema_version": 1,
        "title": "Fleet Dashboard",
        "summary": _summary(by_name),
        "generated_at": generated_at,
        "projects": [{
            "id": "fleet",
            "title": "Agent fleet",
            "summary": "Read-only projection of Multica, GitHub pull requests, Hermes cron, agentctl, fleetctl, Prometheus, and this workspace's inbox.",
            "outcome": _outcome(by_name),
        }],
        "sources": sources,
        "panels": panels,
        "layout": {
            "type": "tabs",
            "id": "fleet",
            "items": [
                {"id": "overview", "label": "Overview", "children": [_ref(pid) for pid in overview_ids]},
                {"id": "agent-work", "label": "Agent work", "children": [_ref(host_chart["id"]), _ref(review_table["id"])]},
                {"id": "pull-requests", "label": "Pull requests", "children": [_ref(repo_chart["id"]), _ref(age_chart["id"]), _ref(pr_table["id"])]},
                {"id": "ops-crons", "label": "Ops & crons", "children": [_ref(cron_chart["id"]), _ref(problem_table["id"]), _ref(runs_chart["id"]), _ref(queue_chart["id"]), _ref(alert_chart["id"])]},
                {"id": "hosts", "label": "Hosts", "children": [_ref(host_table["id"])]},
            ],
        },
    }
    return _fit(report)


def changed_besides_generated_at(previous: str, current: dict) -> bool:
    try:
        old = json.loads(previous)
    except json.JSONDecodeError:
        return True
    if not isinstance(old, dict):
        return True
    old.pop("generated_at", None)
    new = json.loads(json.dumps(current))
    new.pop("generated_at", None)
    return canonical(old) != canonical(new)


def canonical(value) -> str:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def _fit(report: dict) -> dict:
    tables = [panel for panel in report["panels"] if panel["type"] == "evidence-table"]
    while len(json.dumps(report).encode()) > 120_000 and any(panel["data"]["rows"] for panel in tables):
        table = max(tables, key=lambda panel: len(panel["data"]["rows"]))
        if not table["data"]["rows"]:
            break
        table["data"]["rows"].pop()
    return report


def _strips(reads: dict, now: datetime, source_ids: list[str], history: list[dict]) -> tuple[list[dict], list[str]]:
    multica, github, hermes, agentctl, fleet = (reads.get(name) for name in ("multica", "github", "hermes", "agentctl", "fleetctl"))
    prometheus = reads.get("prometheus")
    snapshot = headline_snapshot(reads, now)
    decisions = snapshot["decisions_for_david"]
    waiting = snapshot["decisions_waiting"]
    aging = snapshot["aging_reviews"]
    open_prs = snapshot["open_prs"]
    stuck = snapshot["stuck_runs"]
    problems = snapshot["cron_problems"]
    causes = snapshot["fleetctl_causes"]
    firing = snapshot["prometheus_firing"]
    pending = snapshot["prometheus_pending"]

    def metric(label, key, value, detail, tone):
        trend, trend_label = _trend(history, key)
        return _metric(label, value, detail, tone, trend, trend_label)

    first = _panel(
        "overview-now", "metric-strip", "What the fleet is showing",
        {"items": [
            metric("Decisions for David", "decisions_for_david", decisions,
                   "Inbox asks, red CI, requested changes, and paused watchdogs. Aging reviews are separate.",
                   _tone(decisions)),
            metric("Decisions waiting", "decisions_waiting", waiting, "Open ANX inbox asks", _tone(waiting)),
            metric("Aging reviews", "aging_reviews", aging, "Multica in_review older than 72h", _tone(aging)),
            metric("Open pull requests", "open_prs", open_prs, "Authored by the workspace user", "neutral"),
            metric("Stuck agent runs", "stuck_runs", stuck, "Nonterminal, unreachable or unknown, idle over 24h", _tone(stuck)),
            metric("Cron problems", "cron_problems", problems, "Paused with no reason, error, or missing workdir", _tone(problems)),
        ]},
        _fresh(multica, github, hermes, agentctl, reads.get("nexus")),
        _earliest(multica, github, hermes, agentctl, reads.get("nexus")),
        source_ids,
    )
    second = _panel(
        "overview-fleetctl", "metric-strip", "Fleetctl and Prometheus",
        {"items": [
            metric("Host-action root causes", "fleetctl_causes", causes, "fleetctl host-action queue", _tone(causes)),
            metric("Reporting", "fleetctl_reporting", _inventory(fleet, "reporting"), _reporting_detail(fleet), "neutral"),
            metric("Mute", "fleetctl_mute", _inventory(fleet, "mute"), "Hosts muted", _tone(_inventory(fleet, "mute"))),
            metric("Asleep", "fleetctl_asleep", _inventory(fleet, "asleep"), "Hosts asleep", "neutral"),
            metric("Firing alerts", "prometheus_firing", firing, "Prometheus alerts in firing state", _tone(firing)),
            metric("Pending alerts", "prometheus_pending", pending, "Counted only. Pending is not a card and not zero when unread.", "neutral"),
        ]},
        _fresh(fleet, prometheus),
        _earliest(fleet, prometheus),
        [],
    )
    return [first, second], [first["id"], second["id"]]


def _callout(reads: dict, now: datetime) -> dict:
    lines = _brief_lines(reads, now)
    failed = [name for name, read in reads.items() if read and not read.get("ok")]
    if failed:
        lines.append("Unavailable: " + ", ".join(failed) + ". Unavailable is not healthy or empty.")
    lines = lines[:8]
    if not lines:
        text = "Nothing in the sources read this run needs a decision."
        tone = "info"
    elif failed and len(lines) == 1:
        text = lines[0]
        tone = "warning"
    else:
        text = "\n".join(lines)
        tone = "critical" if any(not line.startswith("Unavailable:") for line in lines) else "warning"
    return _panel(
        "needs-david", "callout", "Needs David now",
        {"tone": tone, "label": "Operator attention", "text": text[:4000]},
        "unavailable" if failed else "current",
        _earliest(*[read for read in reads.values() if read]),
        [],
    )


def _brief_lines(reads: dict, now: datetime) -> list[str]:
    """Ranked attention lines. Each names a count and at most three examples. Zeros are omitted."""
    lines = []
    inbox = _inbox_items(reads)
    if inbox is not None and inbox:
        oldest = min(inbox, key=lambda item: item.get("created_at") or "9999")
        age = _age(oldest.get("created_at"), now)
        shown = oldest["title"][:80]
        if age != "unknown":
            shown = f"{shown} ({age})"
        lines.append(f"{len(inbox)} decisions waiting in your ANX Inbox — oldest: {shown}")
    loose = _loose_items(reads)
    if loose is not None and loose:
        david = [item for item in loose if str(item.get("next_actor") or "").lower() == "david"]
        examples = ", ".join(item["title"][:48] for item in (david or loose)[:3])
        lines.append(
            f"{len(loose)} loose ends on the decisions board — {len(david)} next actor David "
            f"({examples}) — triage, don't read each"
        )
    red, changes = _pr_attention(reads, now)
    if red is not None and (red or changes):
        bits = []
        if red:
            bits.append(f"{len(red)} non-draft PRs with red CI: {', '.join(item['native_id'] for item in red[:3])}")
        if changes:
            bits.append(f"{len(changes)} requested changes: {', '.join(item['native_id'] for item in changes[:3])}")
        lines.append("; ".join(bits))
    review = _review_items(reads)
    if review is not None and review:
        aging = [item for item in review if item.get("in_review_over_72h")]
        oldest = min(review, key=lambda item: item.get("updated_at") or "9999")
        ident = oldest.get("identifier") or oldest.get("native_id")
        lines.append(
            f"{len(review)} Multica issues in review — {len(aging)} older than 72h "
            f"(oldest {ident}, {_age(oldest.get('updated_at'), now)}) — triage, don't read each"
        )
    firing = _firing_items(reads)
    if firing is not None and firing:
        examples = ", ".join(item.get("title") or item["native_id"] for item in firing[:3])
        lines.append(f"Prometheus: {len(firing)} firing (critical first): {examples}")
    watchdogs = _watchdogs(reads)
    if watchdogs is not None and watchdogs:
        examples = ", ".join(f"{row.get('name') or row.get('id')} on {row.get('host')}" for row in watchdogs[:3])
        lines.append(f"Paused safety watchdogs: {len(watchdogs)} — {examples}")
    stuck = _stuck_hosts(reads)
    if stuck is not None and stuck[0]:
        total, hosts = stuck
        if len(hosts) == 1:
            shown = hosts[0][0]
        else:
            shown = ", ".join(f"{label} {count}" for label, count in hosts[:3])
        lines.append(f"Stuck agent runs: {total} on {shown}")
    return lines


def _phase_chart(reads: dict, now: datetime, source_ids: list[str]) -> dict:
    series = []
    for name, label, counter in (
        ("multica", "Multica", lambda read: _phase_counts(read, lambda item: MULTICA_PHASE.get(item.get("status"), "unknown"))),
        ("github", "GitHub", lambda read: _phase_counts(read, lambda item: github_phase(item, now))),
        ("hermes", "Hermes", lambda read: _blocked_only(_problem_total(read))),
        ("agentctl", "agentctl", lambda read: _blocked_only(_card_total(read))),
        ("fleetctl", "fleetctl", lambda read: _blocked_only(None if not read or not read.get("ok") else len(read.get("items") or []))),
        ("prometheus", "Prometheus", lambda read: _blocked_only(None if not read or not read.get("ok") else len(read.get("items") or []))),
    ):
        counts = counter(reads.get(name))
        series.append({
            "type": "bar", "name": label, "stack": "phase",
            "data": [None if counts is None else counts[phase] for phase in OPEN_PHASES],
        })
    return _chart(
        "open-by-phase", "Open work by phase",
        {"caption": "Done and cancelled items are omitted. Null means that source was not read.",
         "palette": "ocean",
         "option": {
             "legend": {"show": True},
             "xAxis": {"type": "category", "data": list(OPEN_PHASES)},
             "yAxis": {"type": "value", "name": "Cards"},
             "series": series,
         }},
        _fresh(*reads.values()),
        _earliest(*reads.values()),
        source_ids,
    )


def _multica_host_chart(read: dict | None) -> dict:
    statuses = ("backlog", "todo", "in_progress", "in_review", "blocked")
    if not read or not read.get("ok"):
        data = {status: [None] for status in statuses}
        categories = ["Multica"]
    else:
        counts: dict[str, dict[str, int]] = {}
        for item in read.get("items") or []:
            if item.get("status") not in statuses:
                continue
            host = item.get("host") or "unassigned"
            counts.setdefault(host, {status: 0 for status in statuses})
            counts[host][item["status"]] += 1
        categories = sorted(counts) or ["none"]
        if not counts:
            counts["none"] = {status: 0 for status in statuses}
        data = {status: [counts[host][status] for host in categories] for status in statuses}
    return _chart(
        "multica-by-host", "Multica status by host",
        {"caption": "Assignee names are reduced to the host prefix. Unassigned work stays visible.",
         "palette": "ocean",
         "option": {
             "legend": {"show": True},
             "xAxis": {"type": "category", "data": categories},
             "yAxis": {"type": "value", "name": "Issues"},
             "series": [{"type": "bar", "name": status, "stack": "status", "data": data[status]} for status in statuses],
         }},
        _fresh(read), _earliest(read),
    )


def _review_table(read, now, add_source, aggregate) -> dict:
    rows = []
    cited = []
    if read and read.get("ok"):
        chosen = [item for item in read.get("items") or [] if item.get("status") in {"in_review", "blocked"}]
        chosen.sort(key=lambda item: (not item.get("in_review_over_72h"), item.get("updated_at") or ""))
        for item in chosen[:120]:
            sid = add_source(
                f"multica-{item['native_id']}",
                str(item.get("identifier") or item["native_id"]),
                item.get("url"), read.get("observed_at"), "issue",
            )
            source_ids = [sid] if sid else ([aggregate] if aggregate else [])
            if sid and sid not in cited:
                cited.append(sid)
            rows.append({
                "cells": [
                    str(item.get("identifier") or ""),
                    str(item.get("title") or "")[:160],
                    str(item.get("host") or "unassigned"),
                    str(item.get("status") or ""),
                    _age(item.get("updated_at"), now),
                    ",".join(item.get("signals") or []) or "none",
                    str(item.get("url") or ""),
                ],
                "source_ids": source_ids,
            })
    if aggregate and aggregate not in cited:
        cited.insert(0, aggregate)
    return _table(
        "multica-attention", "In review and blocked",
        ["Issue", "Title", "Host", "Status", "Age", "Flags", "URL"], rows, cited,
        _fresh(read), _earliest(read),
    )


def _repo_chart(read: dict | None) -> dict:
    if not read or not read.get("ok"):
        categories, data = ["Pull requests"], [None]
    else:
        counts: dict[str, int] = {}
        for item in read.get("items") or []:
            if item.get("terminal"):
                continue
            counts[item.get("repo") or "unknown"] = counts.get(item.get("repo") or "unknown", 0) + 1
        ranked = sorted(counts.items(), key=lambda pair: (-pair[1], pair[0]))
        head, tail = ranked[:24], ranked[24:]
        categories = [name for name, _ in head] or ["No open pull requests"]
        data = [count for _, count in head] or [0]
        if tail:
            categories.append("other repos")
            data.append(sum(count for _, count in tail))
    return _chart(
        "prs-by-repo", "Open pull requests by repository",
        {"caption": "Closed pull requests are excluded.", "palette": "ocean",
         "option": {
             "xAxis": {"type": "category", "data": categories},
             "yAxis": {"type": "value", "name": "Pull requests"},
             "series": [{"type": "bar", "name": "Open", "data": data}],
         }},
        _fresh(read), _earliest(read),
    )


def _age_chart(read: dict | None, now: datetime) -> dict:
    labels = ["under 1d", "1-7d", "7-30d", "over 30d"]
    if not read or not read.get("ok"):
        data = [None, None, None, None]
    else:
        buckets = [0, 0, 0, 0]
        for item in read.get("items") or []:
            if item.get("terminal"):
                continue
            stamp = _parse(item.get("created_at")) or _parse(item.get("updated_at"))
            if stamp is None:
                continue
            days = (now - stamp).total_seconds() / 86400
            index = 0 if days < 1 else 1 if days < 7 else 2 if days <= 30 else 3
            buckets[index] += 1
        data = buckets
    return _chart(
        "pr-age", "Pull request age",
        {"caption": "Age is since creation for open pull requests.", "palette": "ocean",
         "option": {
             "xAxis": {"type": "category", "data": labels},
             "yAxis": {"type": "value", "name": "Pull requests"},
             "series": [{"type": "bar", "name": "Open", "data": data}],
         }},
        _fresh(read), _earliest(read),
    )


def _pr_table(read, now, add_source, aggregate) -> dict:
    rows, cited = [], []
    if read and read.get("ok"):
        chosen = [item for item in read.get("items") or [] if not item.get("terminal") and not item.get("is_draft")]
        chosen.sort(key=lambda item: (item.get("ci") != "red", item.get("updated_at") or ""))
        for item in chosen[:120]:
            sid = None
            if item.get("ci") == "red":
                sid = add_source(
                    f"pr-{item['native_id']}", item["native_id"], item.get("url"),
                    read.get("observed_at"), "pull-request",
                )
            source_ids = [sid] if sid else ([aggregate] if aggregate else [])
            if sid and sid not in cited:
                cited.append(sid)
            rows.append({
                "cells": [
                    str(item.get("repo") or ""),
                    str(item["native_id"]),
                    str(item.get("title") or "")[:120],
                    _age(item.get("created_at"), now),
                    str(item.get("ci") or "unknown"),
                    str(item.get("url") or ""),
                ],
                "source_ids": source_ids,
            })
    if aggregate and aggregate not in cited:
        cited.insert(0, aggregate)
    return _table(
        "pr-ci", "Non-draft pull requests",
        ["Repository", "Pull request", "Title", "Age", "CI", "URL"], rows, cited,
        _fresh(read), _earliest(read),
    )


def _cron_chart(read: dict | None) -> dict:
    names = ("healthy", "paused-no-reason", "error", "missing-workdir")
    hosts = ((read or {}).get("meta") or {}).get("hosts") or []
    if not hosts:
        categories, series = ["cron"], {name: [None] for name in names}
    else:
        categories = _unique_labels([host.get("label") or "host" for host in hosts])
        series = {name: [] for name in names}
        for host in hosts:
            if not host.get("ok"):
                for name in names:
                    series[name].append(None)
                continue
            counts = {name: 0 for name in names}
            for row in host.get("rows") or []:
                bucket = _cron_bucket(row.get("problems") or [])
                if bucket:
                    counts[bucket] += 1
            for name in names:
                series[name].append(counts[name])
    return _chart(
        "cron-health", "Cron health by host",
        {"caption": "A job is counted once: missing workdir, then error, then paused with no reason. Intentional pauses are not called healthy.",
         "palette": "forest",
         "option": {
             "legend": {"show": True},
             "xAxis": {"type": "category", "data": categories},
             "yAxis": {"type": "value", "name": "Jobs"},
             "series": [{"type": "bar", "name": name, "stack": "cron", "data": series[name]} for name in names],
         }},
        _fresh(read), _earliest(read),
    )


def _problem_table(read: dict | None) -> dict:
    rows = []
    for host in ((read or {}).get("meta") or {}).get("hosts") or []:
        if not host.get("ok"):
            continue
        for row in host.get("rows") or []:
            if not row.get("problems"):
                continue
            rows.append({"cells": [
                str(host.get("label") or ""),
                str(row.get("name") or row.get("id") or ""),
                ",".join(row.get("problems") or []),
                str(row.get("last_status") or "unknown"),
                str(row.get("schedule") or ""),
            ], "source_ids": []})
    rows = rows[:200]
    return _table(
        "cron-problems", "Cron problems",
        ["Host", "Job", "Problems", "Last status", "Schedule"], rows, [],
        _fresh(read), _earliest(read),
    )


def _runs_chart(read: dict | None) -> dict:
    hosts = ((read or {}).get("meta") or {}).get("hosts") or []
    categories = _unique_labels([host.get("label") or "host" for host in hosts]) or ["agentctl"]
    def column(key):
        if not hosts:
            return [None]
        values = []
        for host in hosts:
            value = host.get(key) if host.get("ok") else None
            values.append(value if isinstance(value, int) else None)
        return values
    return _chart(
        "agentctl-runs", "agentctl stuck and uncollected runs",
        {"caption": "Stuck means nonterminal, liveness unreachable or unknown, and last update older than 24h. Null means the host was not read.",
         "palette": "sunset",
         "option": {
             "legend": {"show": True},
             "xAxis": {"type": "category", "data": categories},
             "yAxis": {"type": "value", "name": "Runs"},
             "series": [
                 {"type": "bar", "name": "Stuck", "data": column("stuck")},
                 {"type": "bar", "name": "Uncollected", "data": column("uncollected")},
                 {"type": "bar", "name": "Live running", "data": column("live_running")},
             ],
         }},
        _fresh(read), _earliest(read),
    )


def _queue_chart(read: dict | None) -> dict:
    names = ["host-action", "observability", "intent", "contract"]
    queues = ((read or {}).get("meta") or {}).get("queues") or {}
    if read and read.get("ok"):
        names.extend(sorted(key for key in queues if key not in names and isinstance(queues.get(key), int)))
    if not read or not read.get("ok"):
        data = [None for _ in names]
    else:
        data = [queues[name] if isinstance(queues.get(name), int) else None for name in names]
    return _chart(
        "fleetctl-queues", "fleetctl root causes by queue",
        {"caption": "A missing queue is null, not zero. Contract is null when fleetctl does not emit that queue.",
         "palette": "ocean",
         "option": {
             "xAxis": {"type": "category", "data": list(names)},
             "yAxis": {"type": "value", "name": "Root causes"},
             "series": [{"type": "bar", "name": "Root causes", "data": data}],
         }},
        _fresh(read), _earliest(read),
    )


def _alert_chart(read: dict | None) -> dict:
    severities = ("critical", "warning", "info", "other")
    if not read or not read.get("ok"):
        categories, series = ["Prometheus"], {name: [None] for name in severities}
        pending = None
        caption = "Firing and pending are null because Prometheus was not read. That is not zero alerts."
    else:
        counts: dict[str, dict[str, int]] = {}
        for item in read.get("items") or []:
            host = item.get("host") or "unknown"
            bucket = item.get("severity") if item.get("severity") in {"critical", "warning", "info"} else "other"
            counts.setdefault(host, {name: 0 for name in severities})
            counts[host][bucket] += 1
        categories = sorted(counts) or ["none"]
        if not counts:
            counts["none"] = {name: 0 for name in severities}
        series = {name: [counts[host][name] for host in categories] for name in severities}
        pending = (read.get("meta") or {}).get("pending")
        pending_text = str(pending) if isinstance(pending, int) else "unknown"
        caption = f"Firing alerts by severity and host. Pending: {pending_text}. Pending alerts are counted and do not become cards."
    return _chart(
        "prometheus-alerts", "Prometheus alerts by severity and host",
        {"caption": caption, "palette": "sunset",
         "option": {
             "legend": {"show": True},
             "xAxis": {"type": "category", "data": categories},
             "yAxis": {"type": "value", "name": "Firing alerts"},
             "series": [{"type": "bar", "name": name, "stack": "severity", "data": series[name]} for name in severities],
         }},
        _fresh(read), _earliest(read),
    )


def _loose_table(read: dict | None) -> dict:
    rows = []
    if read and (read.get("meta") or {}).get("board_ok"):
        for item in (read.get("meta") or {}).get("loose_ends") or read.get("items") or []:
            rows.append({"cells": [
                str(item.get("title") or "")[:160],
                str(item.get("phase") or "unknown"),
                str(item.get("next_actor") or "unknown"),
                str(item.get("next_action") or "unknown")[:300],
            ], "source_ids": []})
    rows = rows[:120]
    if read and (read.get("meta") or {}).get("board_ok"):
        freshness = "current"
    elif read and not read.get("ok"):
        freshness = "unavailable"
    else:
        freshness = _fresh(read)
    return _table(
        "loose-ends", "Loose ends",
        ["Title", "Phase", "Next actor", "Next action"], rows, [],
        freshness, _earliest(read),
    )


def _host_table(hosts: list[dict], hermes, agentctl, fleet) -> dict:
    hermes_hosts = {host.get("label"): host for host in ((hermes or {}).get("meta") or {}).get("hosts") or []}
    agent_hosts = {host.get("label"): host for host in ((agentctl or {}).get("meta") or {}).get("hosts") or []}
    fleet_hosts = ((fleet or {}).get("meta") or {}).get("hosts") or {}
    rows = []
    for host in hosts:
        label = str(host.get("label") or "")
        hermes_row = hermes_hosts.get(label)
        agent_row = agent_hosts.get(label)
        rows.append({"cells": [
            label,
            _reachable(host, hermes_row, agent_row),
            _jobs(hermes_row),
            str((agent_row or {}).get("version") or "unknown"),
            _int_cell((agent_row or {}).get("stuck") if agent_row and agent_row.get("ok") else None),
            str(fleet_hosts.get(label) or "unknown") if fleet and fleet.get("ok") else "unknown",
        ], "source_ids": []})
    observed = _earliest(hermes, agentctl, fleet)
    return _table(
        "host-inventory", "Hosts",
        ["Host", "Reachable", "Hermes jobs", "agentctl", "Stuck runs", "fleetctl state"],
        rows, [], _fresh(hermes, agentctl, fleet), observed,
    )


def _unavailable_panels(reads: list[dict]) -> list[dict]:
    panels = []
    for read in reads:
        if read.get("ok"):
            continue
        panels.append(_panel(
            _ident(f"unavailable-{read['name']}"), "explanation", f"{read['name']} unavailable",
            {"text": f"{read['name']} could not be read: {read.get('error') or 'unknown error'}. This does not establish health or an empty queue."},
            "unavailable", read.get("observed_at"), [],
        ))
    return panels


def _cron_bucket(problems: list[str]) -> str | None:
    if "missing-workdir" in problems:
        return "missing-workdir"
    if "error" in problems:
        return "error"
    if "paused-no-reason" in problems:
        return "paused-no-reason"
    if not problems:
        return "healthy"
    return None


def _phase_counts(read: dict | None, phase_of) -> dict | None:
    if not read or not read.get("ok"):
        return None
    counts = {phase: 0 for phase in OPEN_PHASES}
    for item in read.get("items") or []:
        phase = phase_of(item)
        if phase in counts:
            counts[phase] += 1
    return counts


def _blocked_only(total: int | None) -> dict | None:
    if total is None:
        return None
    counts = {phase: 0 for phase in OPEN_PHASES}
    counts["blocked"] = total
    return counts


def _count(read: dict | None, predicate) -> int | None:
    if not read or not read.get("ok"):
        return None
    return sum(1 for item in read.get("items") or [] if predicate(item))


def _problem_total(read: dict | None) -> int | None:
    if not read or not read.get("ok"):
        return None
    hosts = (read.get("meta") or {}).get("hosts") or []
    if any(not host.get("ok") for host in hosts):
        return None
    return sum(1 for host in hosts for row in host.get("rows") or [] if row.get("problems"))


def _card_total(read: dict | None) -> int | None:
    if not read or not read.get("ok"):
        return None
    hosts = (read.get("meta") or {}).get("hosts") or []
    if any(not host.get("ok") or host.get("over_threshold") is None for host in hosts):
        return None
    return sum(1 for host in hosts if host.get("over_threshold"))


def _stuck_total(read: dict | None) -> int | None:
    if not read or not read.get("ok"):
        return None
    hosts = (read.get("meta") or {}).get("hosts") or []
    if any(not host.get("ok") or not isinstance(host.get("stuck"), int) for host in hosts):
        return None
    return sum(host["stuck"] for host in hosts)


def headline_snapshot(reads: dict, now: datetime) -> dict:
    """Integer headline metrics. A missing key means that source was not read successfully."""
    decisions = _decisions_for_david(reads, now)
    return {
        "decisions_for_david": decisions,
        "decisions_waiting": _inbox_count(reads),
        "aging_reviews": _aging_count(reads),
        "open_prs": _count(reads.get("github"), lambda item: github_phase(item, now) != "done"),
        "stuck_runs": _stuck_total(reads.get("agentctl")),
        "cron_problems": _problem_total(reads.get("hermes")),
        "fleetctl_causes": None if not reads.get("fleetctl") or not reads["fleetctl"].get("ok") else len(reads["fleetctl"].get("items") or []),
        "fleetctl_reporting": _inventory(reads.get("fleetctl"), "reporting"),
        "fleetctl_mute": _inventory(reads.get("fleetctl"), "mute"),
        "fleetctl_asleep": _inventory(reads.get("fleetctl"), "asleep"),
        "prometheus_firing": None if not reads.get("prometheus") or not reads["prometheus"].get("ok") else len(reads["prometheus"].get("items") or []),
        "prometheus_pending": _pending_count(reads),
    }


def record_history(history: list[dict], *, at: str, metrics: dict) -> list[dict]:
    point = {"at": at, "metrics": {key: value for key, value in metrics.items() if isinstance(value, int)}}
    kept = [entry for entry in history if isinstance(entry, dict)]
    kept.append(point)
    return kept[-50:]


def _decisions_for_david(reads: dict, now: datetime) -> int | None:
    inbox = _inbox_count(reads)
    red, changes = _pr_attention(reads, now)
    watchdogs = _watchdogs(reads)
    if inbox is None or red is None or changes is None or watchdogs is None:
        return None
    return inbox + len(red) + len(changes) + len(watchdogs)


def _inbox_items(reads: dict) -> list[dict] | None:
    nexus = reads.get("nexus")
    if not nexus or not (nexus.get("meta") or {}).get("inbox_ok"):
        return None
    inbox = (nexus.get("meta") or {}).get("inbox")
    return inbox if isinstance(inbox, list) else None


def _inbox_count(reads: dict) -> int | None:
    inbox = _inbox_items(reads)
    return None if inbox is None else len(inbox)


def _loose_items(reads: dict) -> list[dict] | None:
    nexus = reads.get("nexus")
    if not nexus or not (nexus.get("meta") or {}).get("board_ok"):
        return None
    items = (nexus.get("meta") or {}).get("loose_ends")
    if items is None:
        items = nexus.get("items")
    return items if isinstance(items, list) else None


def _aging_count(reads: dict) -> int | None:
    review = _review_items(reads)
    if review is None:
        return None
    return sum(1 for item in review if item.get("in_review_over_72h"))


def _review_items(reads: dict) -> list[dict] | None:
    multica = reads.get("multica")
    if not multica or not multica.get("ok"):
        return None
    return [item for item in multica.get("items") or [] if item.get("status") == "in_review"]


def _pr_attention(reads: dict, now: datetime) -> tuple[list[dict] | None, list[dict] | None]:
    github = reads.get("github")
    if not github or not github.get("ok"):
        return None, None
    red, changes = [], []
    for item in github.get("items") or []:
        if item.get("terminal") or item.get("is_draft"):
            continue
        if item.get("ci") == "red":
            red.append(item)
        elif item.get("review_decision") == "CHANGES_REQUESTED":
            changes.append(item)
    red.sort(key=lambda item: item.get("native_id") or "")
    changes.sort(key=lambda item: item.get("native_id") or "")
    return red, changes


def _firing_items(reads: dict) -> list[dict] | None:
    prometheus = reads.get("prometheus")
    if not prometheus or not prometheus.get("ok"):
        return None
    return list(prometheus.get("items") or [])


def _pending_count(reads: dict) -> int | None:
    prometheus = reads.get("prometheus")
    if not prometheus or not prometheus.get("ok"):
        return None
    value = (prometheus.get("meta") or {}).get("pending")
    return value if isinstance(value, int) else None


def _watchdogs(reads: dict) -> list[dict] | None:
    hermes = reads.get("hermes")
    if not hermes or not hermes.get("ok"):
        return None
    found = []
    for host in (hermes.get("meta") or {}).get("hosts") or []:
        if not host.get("ok"):
            return None
        for row in host.get("rows") or []:
            if row.get("watchdog") and row.get("paused"):
                found.append({**row, "host": host.get("label")})
    return found


def _stuck_hosts(reads: dict) -> tuple[int, list[tuple[str, int]]] | None:
    agentctl = reads.get("agentctl")
    if not agentctl or not agentctl.get("ok"):
        return None
    hosts = []
    total = 0
    for host in (agentctl.get("meta") or {}).get("hosts") or []:
        if not host.get("ok") or not isinstance(host.get("stuck"), int):
            return None
        if host["stuck"] > 0:
            hosts.append((str(host.get("label") or "host"), host["stuck"]))
            total += host["stuck"]
    hosts.sort(key=lambda pair: (-pair[1], pair[0]))
    return total, hosts


def _reporting_detail(read: dict | None) -> str:
    detail = "Hosts reporting"
    other = ((read or {}).get("meta") or {}).get("other_states") or {}
    if read and read.get("ok") and isinstance(other, dict) and other:
        bits = [f"{other[name]} {name}" for name in sorted(other) if isinstance(other[name], int)]
        if bits:
            detail = "Hosts reporting. Also " + ", ".join(bits)
    return detail


def _trend(history: list[dict], key: str) -> tuple[list[int] | None, str | None]:
    points = []
    first = None
    for entry in history:
        if not isinstance(entry, dict):
            continue
        metrics = entry.get("metrics") if isinstance(entry.get("metrics"), dict) else {}
        value = metrics.get(key)
        if not isinstance(value, int) or isinstance(value, bool):
            continue
        if first is None and isinstance(entry.get("at"), str):
            first = entry["at"]
        points.append(value)
    if len(points) < 2:
        return None, None
    points = points[-50:]
    label = f"Last {len(points)} runs"
    if first:
        label = f"Last {len(points)} runs since {first}"
    return points, label[:200]


def _inventory(read: dict | None, key: str) -> int | None:
    if not read or not read.get("ok"):
        return None
    value = ((read.get("meta") or {}).get("inventory") or {}).get(key)
    return value if isinstance(value, int) else None


def _metric(label: str, value: int | None, detail: str, tone: str,
            trend: list[int] | None = None, trend_label: str | None = None) -> dict:
    item = {"label": label, "value": "unknown" if value is None else str(value), "detail": detail, "tone": tone}
    if trend and trend_label and len(trend) >= 2:
        item["trend"] = trend
        item["trend_label"] = trend_label
    return item


def _tone(value: int | None, bad: bool = True) -> str:
    if value is None:
        return "neutral"
    if value > 0:
        return "negative" if bad else "neutral"
    return "positive"


def _summary(reads: dict) -> str:
    failed = [name for name, read in reads.items() if not read.get("ok")]
    if failed:
        return "Fleet snapshot with unavailable sources: " + ", ".join(failed) + ". Unavailable is not healthy or empty."
    return "Fleet snapshot from complete reads of the selected sources. Counts are reported observations, not verified outcomes."


def _outcome(reads: dict) -> str:
    failed = [name for name, read in reads.items() if not read.get("ok")]
    if failed:
        return "Incomplete; " + ", ".join(failed) + " unavailable"
    return "Reported; not independently verified"


def _reachable(host: dict, hermes_row, agent_row) -> str:
    for row in (hermes_row, agent_row):
        if row and str(row.get("error") or "").startswith("ssh "):
            return "no"
    if hermes_row or agent_row:
        return "yes"
    return "unknown"


def _jobs(row) -> str:
    if not row or not row.get("ok") or not isinstance(row.get("jobs"), int):
        return "unknown"
    return str(row["jobs"])


def _int_cell(value) -> str:
    return str(value) if isinstance(value, int) else "unknown"


def _age(value, now: datetime) -> str:
    stamp = _parse(value)
    if stamp is None:
        return "unknown"
    hours = int((now - stamp).total_seconds() // 3600)
    if hours < 48:
        return f"{max(hours, 0)}h"
    return f"{hours // 24}d"


def _parse(value) -> datetime | None:
    if not isinstance(value, str):
        return None
    try:
        stamp = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if stamp.tzinfo is None:
        stamp = stamp.replace(tzinfo=timezone.utc)
    return stamp.astimezone(timezone.utc)


def _fresh(*reads) -> str:
    present = [read for read in reads if read]
    if not present:
        return "unknown"
    if any(not read.get("ok") for read in present):
        return "unavailable"
    return "current"


def _earliest(*reads) -> str | None:
    stamps = [read.get("observed_at") for read in reads if read and read.get("observed_at")]
    return min(stamps) if stamps else None


def _panel(pid, kind, title, data, freshness, observed, source_ids) -> dict:
    return {
        "id": pid, "project_id": "fleet", "type": kind, "title": title,
        "author": "fleet-sync", "provenance": "reported",
        "observed_at": observed, "freshness": freshness,
        "source_ids": [sid for sid in source_ids if sid],
        "density": "compact", "data": data,
    }


def _chart(pid, title, data, freshness, observed, source_ids=()) -> dict:
    return _panel(pid, "chart", title, data, freshness, observed, list(source_ids))


def _table(pid, title, columns, rows, source_ids, freshness, observed) -> dict:
    allowed = set(source_ids)
    cleaned = []
    for row in rows:
        cited = [sid for sid in row["source_ids"] if sid in allowed]
        cleaned.append({"cells": row["cells"], "source_ids": cited})
    return _panel(pid, "evidence-table", title, {"columns": columns, "rows": cleaned}, freshness, observed, list(source_ids))


def _ref(pid: str) -> dict:
    return {"type": "panel", "panel_id": pid}


def _unique_labels(labels: list[str]) -> list[str]:
    seen: dict[str, int] = {}
    unique = []
    for label in labels:
        base = label or "host"
        seen[base] = seen.get(base, 0) + 1
        unique.append(base if seen[base] == 1 else f"{base}-{seen[base]}")
    return unique


def _ident(raw: str) -> str:
    text = _ID.sub("-", raw).strip("-")
    if not text or not text[0].isalnum():
        text = f"s{text}"
    return text[:80]


def _http(value) -> str | None:
    if not isinstance(value, str) or len(value) > 2048 or re.search(r"\s|\\", value):
        return None
    try:
        parts = urlsplit(value)
    except ValueError:
        return None
    if parts.scheme not in {"http", "https"} or not parts.hostname or parts.username or parts.password:
        return None
    return value
