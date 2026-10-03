"""Read-only GitHub pull request search and bounded CI lookup."""

from __future__ import annotations

import json
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
from typing import Any

from .run import HostExec

SEARCH_FIELDS = "repository,number,title,url,updatedAt,createdAt,isDraft,state"
CI_FIELDS = "statusCheckRollup,reviewDecision,mergeable,isDraft"
RED = {"FAILURE", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "ERROR", "STARTUP_FAILURE"}
PENDING = {"PENDING", "QUEUED", "IN_PROGRESS", "WAITING", "REQUESTED", "STALE"}
GREEN = {"SUCCESS", "SKIPPED", "NEUTRAL"}


def parse_search(payload: Any) -> list[dict]:
    if not isinstance(payload, list):
        raise ValueError("gh search prs did not return a list")
    items = []
    for pr in payload:
        if not isinstance(pr, dict):
            continue
        repo = pr.get("repository") or {}
        name = repo.get("nameWithOwner") if isinstance(repo, dict) else None
        number = pr.get("number")
        if not name or not isinstance(number, int):
            continue
        items.append({
            "native_id": f"{name}#{number}",
            "repo": str(name),
            "number": number,
            "title": str(pr.get("title") or ""),
            "url": pr.get("url") if isinstance(pr.get("url"), str) else None,
            "state": str(pr.get("state") or ""),
            "is_draft": bool(pr.get("isDraft")),
            "updated_at": pr.get("updatedAt"),
            "created_at": pr.get("createdAt"),
            "ci": "unknown",
            "review_decision": None,
        })
    return items


def classify_ci(rollup: Any) -> str:
    if not isinstance(rollup, list) or not rollup:
        return "none"
    conclusions = []
    for check in rollup:
        if not isinstance(check, dict):
            continue
        conclusion = check.get("conclusion") or check.get("state")
        status = str(check.get("status") or "").upper()
        if isinstance(conclusion, str) and conclusion.upper() in RED:
            conclusions.append("red")
        elif status in PENDING or (isinstance(conclusion, str) and conclusion.upper() in PENDING):
            conclusions.append("pending")
        elif isinstance(conclusion, str) and conclusion.upper() in GREEN:
            conclusions.append("green")
        elif status and status != "COMPLETED":
            conclusions.append("pending")
        elif conclusion:
            conclusions.append("pending")
    if not conclusions:
        return "none"
    if "red" in conclusions:
        return "red"
    if "pending" in conclusions:
        return "pending"
    return "green"


def _parse_time(value: str | None) -> datetime | None:
    if not isinstance(value, str):
        return None
    try:
        stamp = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if stamp.tzinfo is None:
        stamp = stamp.replace(tzinfo=timezone.utc)
    return stamp.astimezone(timezone.utc)


def read_github(exec_: HostExec, config: dict, *, now: datetime) -> dict:
    observed = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    del config
    try:
        open_prs = _search(exec_, "open")
        closed_prs = _search(exec_, "closed")
        recent_closed = []
        for pr in closed_prs:
            updated = _parse_time(pr.get("updated_at"))
            if updated is not None and now - updated <= timedelta(days=7):
                pr["terminal"] = True
                recent_closed.append(pr)
        for pr in open_prs:
            pr["terminal"] = False
        _attach_ci(exec_, open_prs, now)
        items = open_prs + recent_closed
        complete = len(open_prs) < 200
        return {
            "name": "github",
            "ok": True,
            "complete": complete,
            "observed_at": observed,
            "error": None,
            "items": items,
            "present_ids": [item["native_id"] for item in items],
            "meta": {"open_truncated": not complete},
        }
    except (RuntimeError, ValueError, OSError) as exc:
        return {
            "name": "github",
            "ok": False,
            "complete": False,
            "observed_at": observed,
            "error": str(exc)[:500],
            "items": [],
            "present_ids": [],
            "meta": {},
        }


def _search(exec_: HostExec, state: str) -> list[dict]:
    result = exec_.runner(
        ["gh", "search", "prs", "--author", "@me", "--state", state, "--limit", "200", "--json", SEARCH_FIELDS],
        timeout=60,
    )
    if not result.ok:
        raise RuntimeError(result.error or f"gh search prs --state {state} failed")
    try:
        payload = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise ValueError("gh search prs returned invalid JSON") from exc
    return parse_search(payload)


def _attach_ci(exec_: HostExec, pulls: list[dict], now: datetime) -> None:
    candidates = []
    for pr in pulls:
        updated = _parse_time(pr.get("updated_at"))
        if updated is not None and now - updated <= timedelta(days=3) and pr.get("url"):
            candidates.append(pr)
    candidates.sort(key=lambda pr: pr.get("updated_at") or "", reverse=True)
    selected = candidates[:40]

    def view(pr: dict) -> tuple[dict, str, str | None]:
        result = exec_.runner(
            ["gh", "pr", "view", pr["url"], "--json", CI_FIELDS],
            timeout=25,
        )
        if not result.ok:
            return pr, "unknown", None
        try:
            payload = json.loads(result.stdout)
        except json.JSONDecodeError:
            return pr, "unknown", None
        decision = payload.get("reviewDecision")
        return pr, classify_ci(payload.get("statusCheckRollup")), decision if isinstance(decision, str) else None

    if not selected:
        return
    with ThreadPoolExecutor(max_workers=4) as pool:
        for pr, ci, decision in pool.map(view, selected):
            pr["ci"] = ci
            pr["review_decision"] = decision
