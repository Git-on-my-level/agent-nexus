"""Stable JSON contract between anx-agent-bridge and user-authored subprocess adapters."""

from __future__ import annotations

import json
from typing import Any

from ..models import WakePacket
from .base import AdapterResult

REQUEST_SCHEMA_VERSION = "anx-bridge-adapter-request/v1"
RESPONSE_SCHEMA_VERSION = "anx-bridge-adapter-response/v1"

MODE_DISPATCH = "dispatch"
MODE_DOCTOR = "doctor"

ENV_BRIDGE_MODE = "ANX_BRIDGE_MODE"


def build_dispatch_request(
    *,
    wake_packet: WakePacket,
    prompt_text: str,
    session_key: str,
    existing_native_session_id: str | None,
    adapter_settings: dict[str, Any],
) -> dict[str, Any]:
    return {
        "schema_version": REQUEST_SCHEMA_VERSION,
        "mode": MODE_DISPATCH,
        "wake_packet": wake_packet.to_content(),
        "prompt_text": prompt_text,
        "session_key": session_key,
        "existing_native_session_id": existing_native_session_id,
        "adapter": adapter_settings,
    }


def build_doctor_request(
    *,
    handle: str,
    workspace_id: str,
    adapter_settings: dict[str, Any],
) -> dict[str, Any]:
    return {
        "schema_version": REQUEST_SCHEMA_VERSION,
        "mode": MODE_DOCTOR,
        "handle": handle,
        "workspace_id": workspace_id,
        "adapter": adapter_settings,
    }


def parse_dispatch_response(stdout: str) -> AdapterResult:
    data = _parse_json_object(stdout)
    ver = str(data.get("schema_version", "")).strip()
    if ver != RESPONSE_SCHEMA_VERSION:
        raise ValueError(
            f"adapter response schema_version must be {RESPONSE_SCHEMA_VERSION!r}, got {ver!r}"
        )
    response_text = data.get("response_text")
    if response_text is None:
        raise ValueError("adapter response missing required key: response_text")
    if not isinstance(response_text, str):
        raise ValueError("adapter response response_text must be a JSON string")
    native = data.get("native_session_id")
    native_session_id: str | None
    if native is None or native == "":
        native_session_id = None
    else:
        native_session_id = str(native)
    metadata = data.get("metadata")
    meta: dict[str, Any] | None
    if metadata is None:
        meta = None
    elif isinstance(metadata, dict):
        meta = dict(metadata)
    else:
        raise ValueError("adapter response metadata must be an object or null")
    return AdapterResult(response_text=str(response_text), native_session_id=native_session_id, metadata=meta)


def parse_doctor_response(stdout: str) -> dict[str, Any]:
    data = _parse_json_object(stdout)
    ver = str(data.get("schema_version", "")).strip()
    if ver != RESPONSE_SCHEMA_VERSION:
        raise ValueError(
            f"adapter doctor response schema_version must be {RESPONSE_SCHEMA_VERSION!r}, got {ver!r}"
        )
    ok = data.get("ok")
    if not isinstance(ok, bool):
        raise ValueError("adapter doctor response requires boolean ok")
    out: dict[str, Any] = {"ok": ok}
    if "message" in data:
        out["message"] = str(data["message"])
    details = data.get("details")
    if details is not None:
        if not isinstance(details, dict):
            raise ValueError("adapter doctor details must be an object or omitted")
        out["details"] = dict(details)
    return out


def _parse_json_object(stdout: str) -> dict[str, Any]:
    text = stdout.strip()
    if not text:
        raise ValueError("adapter produced empty stdout")
    try:
        data = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ValueError(f"adapter stdout is not valid JSON: {exc}; stdout={text[:500]!r}") from exc
    if not isinstance(data, dict):
        raise ValueError("adapter stdout JSON must be an object")
    return data


def sample_wake_packet_for_contract(
    *,
    handle: str,
    workspace_id: str,
    workspace_name: str,
    anx_base_url: str,
) -> WakePacket:
    """Minimal wake packet for docs and `adapter contract` output."""
    base = anx_base_url.rstrip("/")
    return WakePacket(
        wakeup_id="wake_contract_example",
        handle=handle,
        actor_id="actor_contract_example",
        workspace_id=workspace_id,
        workspace_name=workspace_name,
        thread_id="thread_contract_example",
        thread_title="Example thread",
        trigger_event_id="evt_contract_example",
        trigger_created_at="2026-01-01T00:00:00Z",
        trigger_author_actor_id="actor_human_example",
        trigger_text=f"@{handle} example wake",
        current_summary="Example summary",
        session_key=f"anx:{workspace_id}:thread_contract_example:{handle}",
        anx_base_url=base,
        thread_context_url=f"{base}/threads/thread_contract_example/context",
        thread_workspace_url=f"{base}/threads/thread_contract_example/workspace",
        trigger_event_url=f"{base}/events/evt_contract_example",
        cli_thread_inspect="anx threads inspect --thread-id thread_contract_example --json",
        cli_thread_workspace="anx threads workspace --thread-id thread_contract_example --json",
    )
