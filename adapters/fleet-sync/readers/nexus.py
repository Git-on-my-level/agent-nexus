"""Read-only Agent Nexus inbox and loose-end cards. Never answers, moves, or creates them."""

from __future__ import annotations

from datetime import datetime, timezone

from anx_client import AnxClient, AnxError
from project import operator_name
from readers.run import HostExec

OPEN_PHASES = {"backlog", "ready", "in_progress", "blocked", "review", "unknown"}


def normalize_inbox(item: dict) -> dict | None:
    if not isinstance(item, dict):
        return None
    title = str(item.get("title") or "").strip()
    if not title:
        return None
    created = item.get("created_at")
    return {
        "id": str(item.get("id") or ""),
        "title": title,
        "kind": str(item.get("kind") or ""),
        "created_at": created if isinstance(created, str) else None,
    }


def normalize_loose(card: dict) -> dict | None:
    if not isinstance(card, dict):
        return None
    phase = str(card.get("phase") or card.get("column_key") or "unknown")
    if phase not in OPEN_PHASES:
        return None
    title = str(card.get("title") or "").strip()
    if not title:
        return None
    actor = str(card.get("next_actor") or "").strip() or "unknown"
    action = str(card.get("next_action") or "").strip() or "unknown"
    return {
        "ref": str(card.get("ref") or ""),
        "title": title,
        "phase": phase,
        "next_actor": actor,
        "next_action": action,
        "updated_at": card.get("updated_at") if isinstance(card.get("updated_at"), str) else None,
    }


def on_board(card: dict, board_ref: str) -> bool:
    ref = str(card.get("board_ref") or "")
    if not ref or not board_ref:
        return False
    if ref == board_ref:
        return True
    return ref.split(":")[-1] == board_ref.split(":")[-1]


def read_nexus(exec_: HostExec, config: dict, *, now: datetime) -> dict:
    observed = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    board = str((config.get("nexus") or {}).get("loose_ends_board") or "")
    client = AnxClient(
        str(config.get("anx_binary") or "anx"),
        str(config.get("base_url") or ""),
        str(config.get("agent") or ""),
        runner=exec_.runner,
    )
    inbox, inbox_error = _inbox(client)
    loose, board_error = _loose_ends(client, board, operator_name(config))
    errors = [item for item in (inbox_error, board_error) if item]
    return {
        "name": "nexus",
        "ok": not errors,
        "complete": not errors,
        "observed_at": observed,
        "error": "; ".join(errors)[:500] if errors else None,
        "items": loose,
        "present_ids": [item["ref"] for item in loose if item.get("ref")],
        "meta": {
            "inbox_ok": inbox_error is None,
            "board_ok": board_error is None,
            "inbox": inbox,
            "loose_ends": loose,
        },
    }


def _inbox(client: AnxClient) -> tuple[list[dict], str | None]:
    try:
        # The installed CLI exposes the human queue as `debug inbox list` (inbox.list).
        # Open is the server default. This command is a read; it does not respond.
        result = client.inbox_list()
    except AnxError as exc:
        return [], exc.message
    raw = result.get("items") if isinstance(result, dict) else None
    if not isinstance(raw, list):
        return [], "inbox list did not return items"
    items = [item for item in (normalize_inbox(row) for row in raw) if item]
    return items, None


def _loose_ends(client: AnxClient, board: str, operator: str) -> tuple[list[dict], str | None]:
    if not board:
        return [], "nexus.loose_ends_board is required"
    try:
        cards = client.work_list("nexus")
    except AnxError as exc:
        return [], exc.message
    chosen = []
    for card in cards:
        if not on_board(card, board):
            continue
        item = normalize_loose(card)
        if item:
            chosen.append(item)
    chosen.sort(key=lambda item: (
        0 if item["next_actor"].lower() == operator.lower() else 1,
        {"blocked": 0, "review": 1, "in_progress": 2, "ready": 3, "backlog": 4}.get(item["phase"], 5),
        item["title"],
    ))
    return chosen, None
