"""Read-only fleetctl status projection. Host-action root causes become cards."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from typing import Any

from .run import HostExec, valid_remote_config, valid_ssh_alias

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
    other: dict[str, int] = {}
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
        elif state:
            other[state] = other.get(state, 0) + 1
    return {"queues": queues, "causes": causes, "inventory": states, "other_states": other, "hosts": by_host}


def withheld_reason(payload: Any) -> str | None:
    """Why this status must not close absent cards.

    inventory.untrusted counts reporting hosts whose reports were quarantined.
    trust.untrusted is accepted for the same count when a newer fleetctl puts
    it there. A non-empty queue caveat means findings were withheld. A mute
    host is not untrusted: fleetctl sets trusted false for every non-reporting
    host, so that flag is not the signal.
    """
    if not isinstance(payload, dict):
        return None
    trust = payload.get("trust") if isinstance(payload.get("trust"), dict) else {}
    reason = _untrusted_count(trust.get("untrusted"))
    if reason:
        return reason
    inventory = payload.get("inventory") if isinstance(payload.get("inventory"), dict) else {}
    reason = _untrusted_count(inventory.get("untrusted"))
    if reason:
        return reason
    for queue in payload.get("queues") or []:
        if not isinstance(queue, dict):
            continue
        caveat = queue.get("caveat")
        if isinstance(caveat, str) and caveat.strip():
            return caveat.strip()[:300]
    return None


def _untrusted_count(value: Any) -> str | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, int) and value > 0:
        return f"{value} untrusted reports"
    if isinstance(value, list) and value:
        return f"{len(value)} untrusted reports"
    return None


def read_fleetctl(exec_: HostExec, config: dict, *, now: datetime) -> dict:
    observed = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    fleet = config.get("fleetctl") or {}
    binary = str(fleet.get("binary") or "fleetctl")
    contract = str(fleet.get("contract_dir") or "")
    reports = str(fleet.get("reports_dir") or "")
    if not contract or not reports:
        return _fail(observed, "fleetctl contract_dir and reports_dir are required")
    alias = str(fleet.get("ssh") or "").strip()
    argv = [binary, "status", "--contract", contract, "--reports", reports, "--format", "json", "--all"]
    if alias:
        if (
            not valid_ssh_alias(alias)
            or not valid_remote_config(binary)
            or not valid_remote_config(contract)
            or not valid_remote_config(reports)
        ):
            return _fail(observed, "fleetctl ssh alias and paths must match a strict pattern")
        # The ssh wrapper quotes every argument and expands a $HOME path itself.
        # The subprocess timeout is the hard kill if that session hangs.
        result = exec_.run({"ssh": alias}, argv, timeout=40)
    else:
        result = exec_.runner(argv, timeout=40)
    if not result.ok:
        return _fail(observed, result.error or "fleetctl status failed")
    try:
        payload = json.loads(result.stdout)
        parsed = parse_status(payload)
    except (json.JSONDecodeError, ValueError) as exc:
        return _fail(observed, str(exc))
    withheld = withheld_reason(payload)
    return {
        "name": "fleetctl",
        "ok": True,
        "complete": withheld is None,
        "observed_at": observed,
        "error": None,
        "items": parsed["causes"],
        "present_ids": [item["native_id"] for item in parsed["causes"]],
        "meta": {
            "queues": parsed["queues"],
            "inventory": parsed["inventory"],
            "other_states": parsed["other_states"],
            "hosts": parsed["hosts"],
            "withheld": withheld,
        },
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
        "meta": {
            "queues": {},
            "inventory": {"reporting": None, "mute": None, "asleep": None},
            "other_states": {},
            "hosts": {},
        },
    }
