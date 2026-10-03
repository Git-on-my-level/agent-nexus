"""Read-only agentctl recent/inbox counts. Prompts and results are never requested."""

from __future__ import annotations

import json
from datetime import datetime, timedelta, timezone
from typing import Any

from .run import HostExec

STUCK_LIVENESS = {"unreachable", "unknown"}


def parse_envelope(payload: Any) -> dict:
    if not isinstance(payload, dict) or payload.get("ok") is not True or not isinstance(payload.get("result"), dict):
        message = ""
        if isinstance(payload, dict):
            message = str((payload.get("error") or {}).get("message") or "")
        raise ValueError(message or "agentctl returned an unsuccessful envelope")
    return payload["result"]


def count_runs(executions: list[dict], *, now: datetime, complete: bool) -> dict:
    if not complete:
        return {"stuck": None, "live_running": None}
    stuck = 0
    live = 0
    for execution in executions:
        state = str(execution.get("state") or "")
        liveness = str(execution.get("liveness") or "")
        if state == "running" and liveness == "alive":
            live += 1
        if liveness not in STUCK_LIVENESS:
            continue
        updated = _parse_time(execution.get("updated_at"))
        if updated is None:
            return {"stuck": None, "live_running": live}
        if now - updated > timedelta(hours=24):
            stuck += 1
    return {"stuck": stuck, "live_running": live}


def _parse_time(value: Any) -> datetime | None:
    if not isinstance(value, str):
        return None
    try:
        stamp = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if stamp.tzinfo is None:
        stamp = stamp.replace(tzinfo=timezone.utc)
    return stamp.astimezone(timezone.utc)


def read_agentctl(exec_: HostExec, config: dict, *, now: datetime) -> dict:
    observed = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    hosts = []
    items = []
    errors = []
    for host in config.get("hosts") or []:
        label = str(host.get("label") or "")
        snapshot = _read_host(exec_, host, label, now)
        hosts.append(snapshot)
        if snapshot["ok"] and snapshot.get("item"):
            items.append(snapshot["item"])
        if not snapshot["ok"]:
            errors.append(f"{label}: {snapshot['error']}")
    return {
        "name": "agentctl",
        "ok": bool(hosts) and not errors,
        "complete": bool(hosts) and not errors,
        "observed_at": observed,
        "error": "; ".join(errors)[:500] if errors else None,
        "items": items,
        "present_ids": [],
        "meta": {"hosts": hosts},
    }


def _read_host(exec_: HostExec, host: dict, label: str, now: datetime) -> dict:
    try:
        version = _version(exec_, host)
        recent = exec_.run(host, ["agentctl", "recent", "--state", "nonterminal", "--limit", "200"], timeout=30)
        if not recent.ok:
            if _missing(recent):
                return _fail(label, "agentctl is not installed")
            return _fail(label, recent.error or "agentctl recent failed")
        result = parse_envelope(json.loads(recent.stdout))
        executions = result.get("executions") if isinstance(result.get("executions"), list) else []
        total = result.get("total")
        complete = isinstance(total, int) and not result.get("has_more") and len(executions) == total
        counts = count_runs(executions, now=now, complete=complete)
        uncollected = _uncollected(exec_, host)
        inbox_note = _inbox(exec_, host)
        stuck = counts["stuck"]
        over = None
        if stuck is not None and uncollected is not None:
            over = stuck > 0 or uncollected > 50
        item = None
        if over:
            item = {
                "native_id": f"{label}/agentctl-hygiene",
                "host": label,
                "stuck": stuck,
                "uncollected": uncollected,
                "live_running": counts["live_running"],
                "version": version,
                "terminal": False,
            }
        return {
            "label": label,
            "ok": True,
            "complete": complete and uncollected is not None,
            "error": None,
            "version": version,
            "stuck": stuck,
            "uncollected": uncollected,
            "live_running": counts["live_running"],
            "over_threshold": over,
            "inbox": inbox_note,
            "item": item,
        }
    except (RuntimeError, ValueError, OSError, json.JSONDecodeError) as exc:
        return _fail(label, str(exc))


def _version(exec_: HostExec, host: dict) -> str | None:
    result = exec_.run(host, ["agentctl", "version"], timeout=15)
    if not result.ok:
        return None
    try:
        payload = json.loads(result.stdout)
        version = parse_envelope(payload).get("version")
    except (json.JSONDecodeError, ValueError):
        return None
    return str(version) if version else None


def _uncollected(exec_: HostExec, host: dict) -> int | None:
    result = exec_.run(host, ["agentctl", "recent", "--unreconciled", "--limit", "1"], timeout=20)
    if not result.ok:
        return None
    try:
        payload = parse_envelope(json.loads(result.stdout))
    except (json.JSONDecodeError, ValueError):
        return None
    total = payload.get("total")
    return total if isinstance(total, int) else None


def _inbox(exec_: HostExec, host: dict) -> str:
    result = exec_.run(host, ["agentctl", "inbox", "--limit", "200"], timeout=25)
    if result.ok:
        try:
            parse_envelope(json.loads(result.stdout))
        except (json.JSONDecodeError, ValueError):
            return "inbox returned an unreadable envelope"
        return "ok"
    text = f"{result.stderr}\n{result.error or ''}".lower()
    if "unknown" in text or "usage" in text:
        return "unsupported"
    return "unavailable"


def _missing(result) -> bool:
    text = f"{result.stderr}\n{result.error or ''}".lower()
    return result.code == 127 or "not found" in text or "no such file" in text


def _fail(label: str, error: str) -> dict:
    return {
        "label": label,
        "ok": False,
        "complete": False,
        "error": error[:300],
        "version": None,
        "stuck": None,
        "uncollected": None,
        "live_running": None,
        "over_threshold": None,
        "inbox": "unavailable",
        "item": None,
    }
