"""Map source items onto work cards and observation facts.

Identity is only (authority, connection_id, native_id). Titles are never used
to match. A done fact always carries evidence. Already-terminal items are not
registered as new cards.
"""

from __future__ import annotations

import hashlib
import json
from datetime import datetime, timedelta, timezone

ADAPTER_VERSION = "0.1.0"
PHASES = {"backlog", "ready", "in_progress", "blocked", "review", "done", "cancelled", "unknown"}
MULTICA_PHASE = {
    "backlog": "backlog",
    "todo": "ready",
    "in_progress": "in_progress",
    "in_review": "review",
    "blocked": "blocked",
    "done": "done",
    "cancelled": "cancelled",
}


def canonical_facts(facts: dict) -> str:
    return json.dumps(facts, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def observation_digest(native_key: str, facts: dict) -> str:
    return hashlib.sha256((native_key + canonical_facts(facts)).encode()).hexdigest()


def native_key(authority: str, connection_id: str, native_id: str) -> str:
    return f"{authority}\n{connection_id}\n{native_id}"


def plan_reads(reads: list[dict], known: dict, config: dict, *, now: datetime) -> list[dict]:
    plans = []
    for read in reads:
        name = read.get("name")
        if name == "multica":
            plans.extend(_multica(read, known, config, now))
        elif name == "github":
            plans.extend(_github(read, known, config, now))
        elif name == "hermes":
            plans.extend(_hermes(read, known, config, now))
        elif name == "agentctl":
            plans.extend(_agentctl(read, known, config, now))
        elif name == "fleetctl":
            plans.extend(_fleetctl(read, known, config, now))
        elif name == "prometheus":
            plans.extend(_prometheus(read, known, config, now))
    return plans


def _finish(plan: dict, known: dict) -> dict:
    facts = plan["facts"]
    if facts.get("phase") not in PHASES:
        raise ValueError(f"invalid phase for {plan['native_id']}")
    if facts["phase"] == "done" and not _has_evidence(plan["evidence"]):
        raise ValueError(f"done fact for {plan['native_id']} is missing evidence")
    key = native_key(plan["authority"], plan["connection_id"], plan["native_id"])
    digest = observation_digest(key, facts)
    plan["digest"] = digest
    plan["reader_id"] = f"fleet-sync/{plan['authority']}"
    previous = known.get((plan["authority"], plan["connection_id"], plan["native_id"]))
    terminal = facts["phase"] in {"done", "cancelled"}
    if previous is None and terminal:
        plan["action"] = "skip"
        plan["reason"] = "terminal item has no card"
        plan["create"] = False
        return plan
    if previous is not None and previous.get("digest") == digest:
        plan["action"] = "skip"
        plan["reason"] = "unchanged"
        plan["create"] = False
        plan["card_ref"] = previous.get("ref")
        return plan
    plan["create"] = previous is None
    plan["action"] = "create" if plan["create"] else "observe"
    plan["reason"] = "new" if plan["create"] else plan.get("reason") or "update"
    plan["card_ref"] = None if previous is None else previous.get("ref")
    return plan


def _has_evidence(evidence: list) -> bool:
    return any(isinstance(entry, dict) and (entry.get("url") or entry.get("ref")) for entry in evidence)


def _card(authority, connection_id, native_id, board, title, summary, owner, phase, native_status,
          url, evidence, facts, reason="update") -> dict:
    return {
        "authority": authority,
        "connection_id": connection_id,
        "native_id": native_id,
        "board_ref": board,
        "title": title[:180],
        "summary": summary[:400],
        "owner": owner or "unassigned",
        "url": url,
        "evidence": evidence,
        "reason": reason,
        "facts": {
            "title": title[:180],
            "phase": phase,
            "native_status": native_status or phase,
            "owner": owner or "unassigned",
            "summary": summary[:400],
            **{key: value for key, value in facts.items() if value is not None},
        },
    }


def _absent(authority, connection_id, native_id, board, known_card, source, now: datetime) -> dict:
    title = (known_card or {}).get("title") or native_id
    owner = (known_card or {}).get("owner") or "unassigned"
    stamp = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    return _card(
        authority, connection_id, native_id, board, title,
        "Absent from a complete source read.", owner, "done", "absent", None,
        [{"ref": f"fleet-sync:{source}:{native_id}:absent-as-of:{stamp}"}],
        {}, reason="absent",
    )


def _cleared(authority, connection_id, native_id, board, known_card, source, summary, now: datetime) -> dict:
    title = (known_card or {}).get("title") or native_id
    owner = (known_card or {}).get("owner") or "unassigned"
    stamp = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    return _card(
        authority, connection_id, native_id, board, title, summary, owner, "done", "cleared", None,
        [{"ref": f"fleet-sync:{source}:{native_id}:cleared-as-of:{stamp}"}],
        {}, reason="cleared",
    )


def _known_for(known: dict, authority: str, connection_id: str) -> dict:
    return {native_id: card for (auth, conn, native_id), card in known.items()
            if auth == authority and conn == connection_id}


def _multica(read: dict, known: dict, config: dict, now: datetime) -> list[dict]:
    if not read.get("ok"):
        return []
    connection = str((config.get("multica") or {}).get("connection_id") or "multica-main")
    board = config["boards"]["agent_work"]
    plans = []
    seen = set()
    for item in read.get("items") or []:
        phase = MULTICA_PHASE.get(item.get("status"), "unknown")
        identifier = item.get("identifier") or item["native_id"]
        title = f"{identifier} · {item.get('title') or ''}".strip(" ·")
        owner = item.get("assignee_name") or "unassigned"
        summary = _multica_summary(item, phase)
        evidence = []
        if phase == "done":
            evidence = [{"url": item["url"]}] if item.get("url") else [
                {"ref": f"fleet-sync:multica:{item['native_id']}:status-done"}
            ]
        extra = {
            "signals": list(item.get("signals") or []),
            "in_review_over_72h": bool(item.get("in_review_over_72h")),
            "host": item.get("host"),
        }
        plans.append(_finish(_card(
            "multica", connection, item["native_id"], board, title, summary, owner, phase,
            item.get("status") or phase, item.get("url"), evidence, extra,
        ), known))
        seen.add(item["native_id"])
    if read.get("complete"):
        for native_id, card in _known_for(known, "multica", connection).items():
            if native_id not in seen:
                plans.append(_finish(_absent("multica", connection, native_id, board, card, "multica", now), known))
    return _stamp(plans, read)


def _multica_summary(item: dict, phase: str) -> str:
    bits = [f"status {item.get('status') or phase}"]
    if item.get("host"):
        bits.append(f"host {item['host']}")
    if item.get("in_review_over_72h"):
        bits.append("in review for more than 72h")
    if item.get("signals"):
        bits.append("flags " + ",".join(item["signals"]))
    return "; ".join(bits)


def github_phase(item: dict, now: datetime) -> str:
    state = str(item.get("state") or "").upper()
    if item.get("terminal") or state in {"CLOSED", "MERGED"}:
        return "done"
    updated = _parse_time(item.get("updated_at"))
    if updated is not None and now - updated > timedelta(days=30):
        return "backlog"
    if item.get("ci") == "red" and not item.get("is_draft"):
        return "blocked"
    if item.get("is_draft"):
        return "in_progress"
    return "review"


def _github(read: dict, known: dict, config: dict, now: datetime) -> list[dict]:
    if not read.get("ok"):
        return []
    connection = str((config.get("github") or {}).get("connection_id") or "github.com")
    owner = str((config.get("github") or {}).get("owner") or "David")
    board = config["boards"]["pull_requests"]
    plans = []
    seen = set()
    for item in read.get("items") or []:
        phase = github_phase(item, now)
        title = f"{item['native_id']} · {item.get('title') or ''}".strip(" ·")
        summary = _github_summary(item, phase)
        evidence = []
        if phase == "done":
            evidence = [{"url": item["url"]}] if item.get("url") else [
                {"ref": f"fleet-sync:github:{item['native_id']}:status-done"}
            ]
        plans.append(_finish(_card(
            "github", connection, item["native_id"], board, title, summary, owner, phase,
            item.get("state") or phase, item.get("url"), evidence,
            {"ci": item.get("ci") or "unknown", "is_draft": bool(item.get("is_draft")),
             "review_decision": item.get("review_decision"), "repo": item.get("repo")},
        ), known))
        seen.add(item["native_id"])
    if read.get("complete"):
        for native_id, card in _known_for(known, "github", connection).items():
            if native_id not in seen:
                plans.append(_finish(_absent("github", connection, native_id, board, card, "github", now), known))
    return _stamp(plans, read)


def _github_summary(item: dict, phase: str) -> str:
    if phase == "backlog":
        return "stale"
    bits = []
    if item.get("is_draft"):
        bits.append("draft")
    ci = item.get("ci") or "unknown"
    bits.append(f"CI {ci}")
    if item.get("review_decision"):
        bits.append(str(item["review_decision"]))
    return "; ".join(bits) or phase


def _hermes(read: dict, known: dict, config: dict, now: datetime) -> list[dict]:
    board = config["boards"]["ops_hygiene"]
    plans = []
    for host in (read.get("meta") or {}).get("hosts") or []:
        label = host.get("label") or ""
        if not host.get("ok"):
            continue
        current = {item["native_id"] for item in host.get("items") or []}
        for item in host.get("items") or []:
            title = item.get("title") or item["native_id"]
            plans.append(_finish(_card(
                "hermes-cron", label, item["native_id"], board, title,
                item.get("summary") or "cron problem", label, "blocked",
                item.get("native_status") or "problem", None, [],
                {"problems": list(item.get("problems") or []), "host": label,
                 "watchdog": bool(item.get("watchdog"))},
            ), known))
        if not host.get("complete"):
            continue
        for native_id, card in _known_for(known, "hermes-cron", label).items():
            if native_id in current:
                continue
            if native_id in set(host.get("job_ids") or []):
                plans.append(_finish(_cleared(
                    "hermes-cron", label, native_id, board, card, "hermes-cron",
                    "Cron problem cleared on a complete read.", now,
                ), known))
            else:
                plans.append(_finish(_absent(
                    "hermes-cron", label, native_id, board, card, "hermes-cron", now,
                ), known))
    return _stamp(plans, read)


def _agentctl(read: dict, known: dict, config: dict, now: datetime) -> list[dict]:
    board = config["boards"]["ops_hygiene"]
    plans = []
    for host in (read.get("meta") or {}).get("hosts") or []:
        label = host.get("label") or ""
        native_id = f"{label}/agentctl-hygiene"
        if not host.get("ok"):
            continue
        stuck = host.get("stuck")
        uncollected = host.get("uncollected")
        if host.get("over_threshold"):
            title = f"agentctl on {label}: {stuck} stuck runs, {uncollected} uncollected"
            summary = f"{stuck} nonterminal runs are unreachable or unknown and older than 24h; {uncollected} terminals are uncollected."
            plans.append(_finish(_card(
                "agentctl", label, native_id, board, title, summary, label, "blocked",
                "over-threshold", None, [],
                {"stuck": stuck, "uncollected": uncollected, "live_running": host.get("live_running"),
                 "host": label},
            ), known))
        elif host.get("complete") and host.get("over_threshold") is False:
            card = _known_for(known, "agentctl", label).get(native_id)
            if card is not None:
                plans.append(_finish(_cleared(
                    "agentctl", label, native_id, board, card, "agentctl",
                    "Stuck runs and uncollected terminals are under the threshold.", now,
                ), known))
    return _stamp(plans, read)


def _fleetctl(read: dict, known: dict, config: dict, now: datetime) -> list[dict]:
    if not read.get("ok"):
        return []
    board = config["boards"]["ops_hygiene"]
    plans = []
    seen = set()
    for item in read.get("items") or []:
        hosts = sorted(item.get("hosts") or [])
        shown = ", ".join(hosts[:8])
        if len(hosts) > 8:
            shown += f" +{len(hosts) - 8}"
        summary = f"priority {item.get('priority') or 'unknown'}; {len(hosts)} hosts"
        if shown:
            summary += f": {shown}"
        plans.append(_finish(_card(
            "fleetctl", "fleet", item["native_id"], board,
            f"fleetctl: {item['native_id']}", summary, "fleet", "blocked",
            item.get("priority") or "root-cause", None, [],
            {"priority": item.get("priority"), "hosts": hosts[:12], "queue": "host-action"},
        ), known))
        seen.add(item["native_id"])
    if read.get("complete"):
        for native_id, card in _known_for(known, "fleetctl", "fleet").items():
            if native_id not in seen:
                plans.append(_finish(_absent("fleetctl", "fleet", native_id, board, card, "fleetctl", now), known))
    return _stamp(plans, read)


def _prometheus(read: dict, known: dict, config: dict, now: datetime) -> list[dict]:
    if not read.get("ok"):
        return []
    connection = str((config.get("prometheus") or {}).get("connection_id") or "prometheus")
    board = config["boards"]["ops_hygiene"]
    plans = []
    seen = set()
    for item in read.get("items") or []:
        host = item.get("host") or "unknown"
        summary = f"severity {item.get('severity') or 'unknown'}; firing on {host}"
        plans.append(_finish(_card(
            "prometheus", connection, item["native_id"], board,
            item.get("title") or item["native_id"], summary, host or "prometheus", "blocked",
            item.get("severity") or "firing", None, [],
            {"severity": item.get("severity"), "host": host, "state": "firing"},
        ), known))
        seen.add(item["native_id"])
    if read.get("complete"):
        for native_id, card in _known_for(known, "prometheus", connection).items():
            if native_id not in seen:
                plans.append(_finish(_absent(
                    "prometheus", connection, native_id, board, card, "prometheus", now,
                ), known))
    return _stamp(plans, read)


def _stamp(plans: list[dict], read: dict) -> list[dict]:
    for plan in plans:
        plan["observed_at"] = read.get("observed_at")
    return plans


def _parse_time(value) -> datetime | None:
    if not isinstance(value, str):
        return None
    try:
        stamp = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if stamp.tzinfo is None:
        stamp = stamp.replace(tzinfo=timezone.utc)
    return stamp.astimezone(timezone.utc)
