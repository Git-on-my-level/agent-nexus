"""Read-only Multica issue and agent lists."""

from __future__ import annotations

import json
import re
from datetime import datetime, timedelta, timezone
from typing import Any

from .run import HostExec

OPEN_STATUSES = ("backlog", "todo", "in_progress", "in_review", "blocked")
TERMINAL_STATUSES = ("done", "cancelled")
_ROLE = re.compile(r"^(?P<host>.+?)\s+(?:Codex|Cursor|Devin|ZCode|Hermes|Helper)\b", re.IGNORECASE)
_SIGNAL = re.compile(r"\b(?:reply\s*ok|smoke|probe)\b", re.IGNORECASE)
_SIGNAL_NAME = {"reply ok": "reply-ok", "replyok": "reply-ok", "smoke": "smoke", "probe": "probe"}


def host_label(name: str) -> str | None:
    match = _ROLE.match((name or "").strip())
    if not match:
        return None
    return re.sub(r"\s+", " ", match.group("host")).strip() or None


def signals(title: str, identifier: str = "") -> list[str]:
    found = []
    for match in _SIGNAL.finditer(f"{identifier} {title}"):
        key = re.sub(r"\s+", " ", match.group(0).lower()).strip()
        key = _SIGNAL_NAME.get(key.replace(" ", ""), _SIGNAL_NAME.get(key, key.replace(" ", "-")))
        if key not in found:
            found.append(key)
    return found


def review_over_72h(status: str, updated_at: str | None, now: datetime) -> bool:
    if status != "in_review" or not updated_at:
        return False
    stamp = _parse_time(updated_at)
    if stamp is None:
        return False
    return now - stamp > timedelta(hours=72)


def issue_url(app_url: str | None, slug: str | None, issue_id: str) -> str | None:
    if not app_url or not slug or not issue_id:
        return None
    return f"{app_url.rstrip('/')}/{slug}/issues/{issue_id}"


def parse_agents(payload: Any) -> dict[str, dict[str, str | None]]:
    agents = payload if isinstance(payload, list) else (payload or {}).get("agents") or []
    out = {}
    for agent in agents:
        if not isinstance(agent, dict) or not agent.get("id"):
            continue
        name = str(agent.get("name") or "")
        out[str(agent["id"])] = {"name": name, "host": host_label(name)}
    return out


def parse_issues(payload: Any) -> tuple[list[dict], bool]:
    if not isinstance(payload, dict):
        raise ValueError("multica issue list did not return an object")
    issues = payload.get("issues")
    if not isinstance(issues, list):
        raise ValueError("multica issue list is missing issues")
    total = payload.get("total")
    truncated = bool(payload.get("has_more")) or (isinstance(total, int) and total > len(issues))
    return [item for item in issues if isinstance(item, dict)], truncated


def normalize_issue(issue: dict, agents: dict[str, dict[str, str | None]], *, app_url: str | None,
                    slug: str | None, now: datetime) -> dict:
    status = str(issue.get("status") or "")
    title = str(issue.get("title") or "")
    identifier = str(issue.get("identifier") or "")
    assignee = agents.get(str(issue.get("assignee_id") or ""), {})
    updated = issue.get("updated_at")
    return {
        "native_id": str(issue.get("id") or ""),
        "project": str(issue.get("project_id") or ""),
        "labels": [str(label.get("name") or label.get("id") or "") if isinstance(label, dict) else str(label) for label in issue.get("labels") or []],
        "identifier": identifier,
        "title": title,
        "status": status,
        "updated_at": updated,
        "created_at": issue.get("created_at"),
        "assignee_name": assignee.get("name"),
        "host": assignee.get("host"),
        "url": issue_url(app_url, slug, str(issue.get("id") or "")),
        "signals": signals(title, identifier),
        "in_review_over_72h": review_over_72h(status, updated if isinstance(updated, str) else None, now),
    }


def _parse_time(value: str) -> datetime | None:
    try:
        stamp = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if stamp.tzinfo is None:
        stamp = stamp.replace(tzinfo=timezone.utc)
    return stamp.astimezone(timezone.utc)


def _load_json(result) -> Any:
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise ValueError("multica returned invalid JSON") from exc


def read_multica(exec_: HostExec, config: dict, *, now: datetime) -> dict:
    observed = _iso(now)
    multica = config.get("multica") or {}
    app_url = multica.get("app_url") or None
    slug = multica.get("workspace_slug") or None
    try:
        agents_run = exec_.runner(["multica", "agent", "list", "--output", "json"], timeout=45)
        if not agents_run.ok:
            raise RuntimeError(agents_run.error or "multica agent list failed")
        agents = parse_agents(_load_json(agents_run))
        if not slug:
            workspace_run = exec_.runner(["multica", "workspace", "get", "--output", "json"], timeout=30)
            if workspace_run.ok:
                payload = _load_json(workspace_run)
                workspace = payload.get("workspace", payload) if isinstance(payload, dict) else {}
                slug = workspace.get("slug") or slug
        items = []
        open_truncated = False
        terminal_truncated = False
        for status in OPEN_STATUSES + TERMINAL_STATUSES:
            listed = exec_.runner(
                ["multica", "issue", "list", "--status", status, "--limit", "200", "--output", "json"],
                timeout=45,
            )
            if not listed.ok:
                raise RuntimeError(listed.error or f"multica issue list {status} failed")
            raw, status_truncated = parse_issues(_load_json(listed))
            # Done history is paged and much larger than the live board. A short
            # first page still holds the recent closes. Only a truncated open
            # status means issues may have disappeared from an incomplete read.
            if status in TERMINAL_STATUSES:
                terminal_truncated = terminal_truncated or status_truncated
            else:
                open_truncated = open_truncated or status_truncated
            for issue in raw:
                item = normalize_issue(issue, agents, app_url=app_url, slug=slug, now=now)
                if not item["native_id"]:
                    continue
                if status in TERMINAL_STATUSES:
                    updated = _parse_time(item["updated_at"]) if isinstance(item["updated_at"], str) else None
                    if updated is None or now - updated > timedelta(days=7):
                        continue
                items.append(item)
        return {
            "name": "multica",
            "ok": True,
            "complete": not open_truncated,
            "observed_at": observed,
            "error": None,
            "items": items,
            "present_ids": [item["native_id"] for item in items],
            "meta": {
                "app_url": app_url,
                "workspace_slug": slug,
                "workspace_url": f"{app_url.rstrip('/')}/{slug}/issues" if app_url and slug else None,
                "truncated": open_truncated,
                "terminal_truncated": terminal_truncated,
            },
        }
    except (RuntimeError, ValueError, OSError) as exc:
        return _failed("multica", observed, str(exc))


def _failed(name: str, observed: str, error: str) -> dict:
    return {
        "name": name,
        "ok": False,
        "complete": False,
        "observed_at": observed,
        "error": error[:500],
        "items": [],
        "present_ids": [],
        "meta": {},
    }


def _iso(now: datetime) -> str:
    return now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
