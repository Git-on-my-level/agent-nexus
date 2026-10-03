"""Read-only fleetctl status projection. Host-action root causes become cards."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from typing import Any

from .run import HostExec

QUEUE_NAMES = ("host-action", "observability", "intent", "contract")


def parse_status(payload: Any) -> dict:
    if not isinstance(payload, dict):
        raise ValueError("fleetctl status did not return an object")
    queues = {}
    causes = []
    for queue in payload.get("queues") or []:
        if not isinstance(queue, dict):
            continue
        name = str(queue.get("queue") or "")
        count = queue.get("root_cause_count")
        queues[name] = count if isinstance(count, int) else None
        if name != "host-action":
            continue
        for cause in queue.get("root_causes") or []:
            if not isinstance(cause, dict) or not cause.get("key"):
                continue
            hosts = [str(host) for host in cause.get("hosts") or [] if isinstance(host, str)]
            refs = [str(ref) for ref in cause.get("refs") or [] if isinstance(ref, str)]
            causes.append({
                "native_id": str(cause["key"]),
                "priority": str(cause.get("priority") or ""),
                "hosts": hosts,
                "refs": refs,
                "terminal": False,
            })
    inventory = payload.get("inventory") if isinstance(payload.get("inventory"), dict) else {}
    states = {"reporting": 0, "mute": 0, "asleep": 0}
    by_host = {}
    for host in inventory.get("hosts") or []:
        if not isinstance(host, dict):
            continue
        name = str(host.get("host") or "")
        state = str(host.get("state") or "")
        if name:
            by_host[name] = state or None
        if state in states:
            states[state] += 1
    return {"queues": queues, "causes": causes, "inventory": states, "hosts": by_host}


def read_fleetctl(exec_: HostExec, config: dict, *, now: datetime) -> dict:
    observed = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    fleet = config.get("fleetctl") or {}
    binary = str(fleet.get("binary") or "fleetctl")
    contract = str(fleet.get("contract_dir") or "")
    reports = str(fleet.get("reports_dir") or "")
    if not contract or not reports:
        return _fail(observed, "fleetctl contract_dir and reports_dir are required")
    result = exec_.runner(
        [binary, "status", "--contract", contract, "--reports", reports, "--format", "json", "--all"],
        timeout=40,
    )
    if not result.ok:
        return _fail(observed, result.error or "fleetctl status failed")
    try:
        parsed = parse_status(json.loads(result.stdout))
    except (json.JSONDecodeError, ValueError) as exc:
        return _fail(observed, str(exc))
    return {
        "name": "fleetctl",
        "ok": True,
        "complete": True,
        "observed_at": observed,
        "error": None,
        "items": parsed["causes"],
        "present_ids": [item["native_id"] for item in parsed["causes"]],
        "meta": {"queues": parsed["queues"], "inventory": parsed["inventory"], "hosts": parsed["hosts"]},
    }


def _fail(observed: str, error: str) -> dict:
    return {
        "name": "fleetctl",
        "ok": False,
        "complete": False,
        "observed_at": observed,
        "error": error[:500],
        "items": [],
        "present_ids": [],
        "meta": {"queues": {}, "inventory": {"reporting": None, "mute": None, "asleep": None}, "hosts": {}},
    }
