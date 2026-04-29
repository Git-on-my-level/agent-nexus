"""Bundled Hermes ACP subprocess adapter for ``anx-agent-bridge``.

This module is intentionally a subprocess entrypoint, not an in-process bridge adapter.
Hermes owns its ACP lifecycle; the Agent Nexus bridge owns wake claiming, auth, and writeback.
Keeping the boundary JSON-over-stdio makes the bundled adapter easy for operators to copy and
modify when they need local Hermes-specific behavior.
"""

from __future__ import annotations

import asyncio
import importlib
import json
import os
import shutil
import subprocess
import sys
from dataclasses import asdict, is_dataclass
from pathlib import Path
from typing import Any

DEFAULT_HERMES_ARGS = ["acp"]
DEFAULT_PROTOCOL_VERSION = 1
MODE_DISPATCH = "dispatch"
MODE_DOCTOR = "doctor"
RESPONSE_SCHEMA_VERSION = "anx-bridge-adapter-response/v1"


def main() -> int:
    try:
        request = json.loads(sys.stdin.read() or "{}")
        if not isinstance(request, dict):
            raise ValueError("adapter request must be a JSON object")
        mode = str(request.get("mode") or os.environ.get("ANX_BRIDGE_MODE") or "").strip()
        settings = request.get("adapter") if isinstance(request.get("adapter"), dict) else {}
        if mode == MODE_DOCTOR:
            _print_response(_doctor(settings))
            return 0
        if mode == MODE_DISPATCH:
            _print_response(asyncio.run(_dispatch(request, settings)))
            return 0
        raise ValueError(f"unsupported adapter mode: {mode!r}")
    except Exception as exc:
        print(f"hermes adapter error: {exc}", file=sys.stderr)
        return 1


def _doctor(settings: dict[str, Any]) -> dict[str, Any]:
    details: dict[str, Any] = {}
    ok = True
    messages: list[str] = []

    hermes_bin = _resolve_hermes_bin(settings)
    details["hermes_bin"] = hermes_bin or ""
    if not hermes_bin:
        ok = False
        messages.append("Hermes binary not found; set HERMES_BIN or [adapter].hermes_bin")
    else:
        version = _probe_version(hermes_bin)
        details["hermes_version"] = version
        if not version:
            messages.append("Hermes binary resolved, but version probing produced no output")

    acp_mod = _import_acp()
    details["acp_importable"] = acp_mod is not None
    if acp_mod is None:
        ok = False
        messages.append("Python package `acp` is not importable in this adapter runtime")

    details["cwd"] = _resolve_cwd(settings)
    details["hermes_args"] = _resolve_hermes_args(settings)
    details["interactive"] = _resolve_bool(settings, "interactive", "HERMES_INTERACTIVE", False)
    return {
        "schema_version": RESPONSE_SCHEMA_VERSION,
        "ok": ok,
        "message": "; ".join(messages) if messages else "Hermes ACP adapter is ready",
        "details": details,
    }


async def _dispatch(request: dict[str, Any], settings: dict[str, Any]) -> dict[str, Any]:
    hermes_bin = _resolve_hermes_bin(settings)
    if not hermes_bin:
        raise RuntimeError("Hermes binary not found; set HERMES_BIN or [adapter].hermes_bin")
    acp_mod = _import_acp()
    if acp_mod is None:
        raise RuntimeError("Python package `acp` is not importable in this adapter runtime")

    collector = _HermesClient(allow_first_permission=_resolve_bool(settings, "interactive", "HERMES_INTERACTIVE", False))
    cmd = [hermes_bin, *_resolve_hermes_args(settings)]
    cwd = _resolve_cwd(settings)
    prompt_text = _build_prompt(request)
    existing_session_id = _clean_string(request.get("existing_native_session_id"))

    async with _spawn_agent_process(acp_mod, collector, cmd) as (conn, _proc):
        await _call_initialize(conn)
        if existing_session_id:
            session_id = await _load_session(conn, cwd, existing_session_id)
        else:
            session_id = await _new_session(conn, cwd)
        prompt_response = await _prompt(conn, session_id, prompt_text, acp_mod)

    response_text = collector.response_text().strip() or _extract_response_text(prompt_response).strip()
    if not response_text:
        response_text = "Hermes completed the ACP turn without returning text."
    return {
        "schema_version": RESPONSE_SCHEMA_VERSION,
        "response_text": response_text,
        "native_session_id": session_id,
        "metadata": {
            "adapter_kind": "hermes",
            "hermes_command": cmd,
            "cwd": cwd,
            "stop_reason": _get_value(prompt_response, "stopReason", "stop_reason"),
        },
    }


class _HermesClient:
    def __init__(self, *, allow_first_permission: bool) -> None:
        self.allow_first_permission = allow_first_permission
        self._chunks: list[str] = []

    async def session_update(self, session_id: str, update: Any, **kwargs: Any) -> None:
        text = _session_update_text(update)
        if text:
            self._chunks.append(text)

    async def request_permission(self, options: Any, session_id: str, tool_call: Any, **kwargs: Any) -> dict[str, Any]:
        if not self.allow_first_permission:
            return {"outcome": {"outcome": "cancelled"}}
        option_id = _first_permission_option_id(options)
        if option_id:
            return {"outcome": {"outcome": "selected", "option_id": option_id}}
        return {"outcome": {"outcome": "cancelled"}}

    def response_text(self) -> str:
        return "".join(self._chunks)


def _resolve_hermes_bin(settings: dict[str, Any]) -> str:
    configured = _clean_string(settings.get("hermes_bin")) or _clean_string(os.environ.get("HERMES_BIN"))
    if configured:
        return str(Path(configured).expanduser())
    return shutil.which("hermes") or ""


def _resolve_hermes_args(settings: dict[str, Any]) -> list[str]:
    raw = settings.get("hermes_args")
    if isinstance(raw, list) and raw:
        return [str(item) for item in raw if str(item).strip()]
    env_args = _clean_string(os.environ.get("HERMES_ARGS"))
    if env_args:
        return env_args.split()
    return list(DEFAULT_HERMES_ARGS)


def _resolve_cwd(settings: dict[str, Any]) -> str:
    raw = _clean_string(os.environ.get("HERMES_CWD")) or _clean_string(settings.get("hermes_cwd")) or _clean_string(settings.get("cwd"))
    if not raw:
        return os.getcwd()
    return str(Path(raw).expanduser().resolve())


def _resolve_bool(settings: dict[str, Any], key: str, env_key: str, default: bool) -> bool:
    raw = os.environ.get(env_key)
    if raw is None:
        raw = settings.get(key)
    if raw is None:
        return default
    return str(raw).strip().lower() in {"1", "true", "yes", "on"}


def _probe_version(hermes_bin: str) -> str:
    for args in ([hermes_bin, "--version"], [hermes_bin, "version"]):
        try:
            proc = subprocess.run(args, check=False, text=True, capture_output=True, timeout=10)
        except (OSError, subprocess.TimeoutExpired):
            continue
        output = (proc.stdout or proc.stderr or "").strip()
        if output:
            return output.splitlines()[0]
    return ""


def _import_acp() -> Any | None:
    try:
        return importlib.import_module("acp")
    except ImportError:
        return None


def _spawn_agent_process(acp_mod: Any, client: _HermesClient, cmd: list[str]) -> Any:
    spawn = getattr(acp_mod, "spawn_agent_process")
    try:
        return spawn(client, *cmd)
    except TypeError:
        return spawn(lambda _agent: client, *cmd)


async def _call_initialize(conn: Any) -> Any:
    if hasattr(conn, "initialize"):
        try:
            return await conn.initialize(protocol_version=DEFAULT_PROTOCOL_VERSION)
        except TypeError:
            pass
        req = _schema_instance("InitializeRequest", protocolVersion=DEFAULT_PROTOCOL_VERSION)
        if req is not None:
            try:
                return await conn.initialize(req)
            except TypeError:
                pass
    return await _send_request(conn, "initialize", {"protocolVersion": DEFAULT_PROTOCOL_VERSION})


async def _new_session(conn: Any, cwd: str) -> str:
    if hasattr(conn, "new_session"):
        try:
            session = await conn.new_session(cwd=cwd, mcp_servers=[])
            return _session_id(session)
        except TypeError:
            pass
    if hasattr(conn, "newSession"):
        req = _schema_instance("NewSessionRequest", cwd=cwd, mcpServers=[])
        if req is not None:
            try:
                session = await conn.newSession(req)
                return _session_id(session)
            except TypeError:
                pass
    session = await _send_request(conn, "session/new", {"cwd": cwd, "mcpServers": []})
    return _session_id(session)


async def _load_session(conn: Any, cwd: str, session_id: str) -> str:
    if hasattr(conn, "load_session"):
        try:
            session = await conn.load_session(cwd=cwd, session_id=session_id, mcp_servers=[])
            return _optional_session_id(session) or session_id
        except TypeError:
            pass
    if hasattr(conn, "loadSession"):
        req = _schema_instance("LoadSessionRequest", cwd=cwd, sessionId=session_id, mcpServers=[])
        if req is not None:
            try:
                session = await conn.loadSession(req)
                return _optional_session_id(session) or session_id
            except TypeError:
                pass
    session = await _send_request(conn, "session/load", {"cwd": cwd, "sessionId": session_id, "mcpServers": []})
    return _optional_session_id(session) or session_id


async def _prompt(conn: Any, session_id: str, prompt_text: str, acp_mod: Any) -> Any:
    block = _text_block(acp_mod, prompt_text)
    if hasattr(conn, "prompt"):
        try:
            return await conn.prompt(session_id=session_id, prompt=[block])
        except TypeError:
            pass
        req = _schema_instance("PromptRequest", sessionId=session_id, prompt=[block])
        if req is not None:
            try:
                return await conn.prompt(req)
            except TypeError:
                pass
    return await _send_request(conn, "session/prompt", {"sessionId": session_id, "prompt": [block]})


async def _send_request(conn: Any, method: str, params: dict[str, Any]) -> Any:
    raw_conn = getattr(conn, "_conn", conn)
    send_request = getattr(raw_conn, "send_request", None)
    if send_request is None:
        raise RuntimeError(f"ACP connection does not expose {method!r} or _conn.send_request")
    return await send_request(method, params)


def _schema_instance(name: str, **kwargs: Any) -> Any | None:
    try:
        schema = importlib.import_module("acp.schema")
        cls = getattr(schema, name)
        return cls(**kwargs)
    except Exception:
        return None


def _text_block(acp_mod: Any, text: str) -> Any:
    helper = getattr(acp_mod, "text_block", None)
    if helper is not None:
        return helper(text)
    return {"type": "text", "text": text}


def _build_prompt(request: dict[str, Any]) -> str:
    packet = request.get("wake_packet") if isinstance(request.get("wake_packet"), dict) else {}
    prompt_text = _clean_string(request.get("prompt_text"))
    lines = [
        "Agent Nexus wake request",
        "",
        f"Handle: {_nested(packet, 'target', 'handle')}",
        f"Workspace: {_nested(packet, 'workspace', 'name')} ({_nested(packet, 'workspace', 'id')})",
        f"Thread: {_nested(packet, 'thread', 'title')} ({_nested(packet, 'thread', 'id')})",
        f"Trigger event: {_nested(packet, 'trigger', 'message_event_id')}",
        f"Trigger text: {_nested(packet, 'trigger', 'text')}",
    ]
    summary = _nested(packet, "context_inline", "current_summary")
    if summary:
        lines.extend(["", "Current summary:", summary])
    preferred = _nested(packet, "context_fetch", "preferred")
    cli_fetch = packet.get("context_fetch", {}).get("cli", []) if isinstance(packet.get("context_fetch"), dict) else []
    if preferred or cli_fetch:
        lines.extend(["", "Context fetch:", f"Preferred: {preferred}"])
        if isinstance(cli_fetch, list):
            lines.extend(str(item) for item in cli_fetch if str(item).strip())
    if prompt_text:
        lines.extend(["", "Bridge prompt:", prompt_text])
    return "\n".join(lines).strip()


def _session_update_text(update: Any) -> str:
    data = _to_plain(update)
    if not isinstance(data, dict):
        return ""
    kind = _clean_string(data.get("sessionUpdate") or data.get("session_update"))
    if kind and kind not in {"agent_message_chunk", "agentMessageChunk"}:
        return ""
    content = data.get("content")
    if isinstance(content, dict):
        if _clean_string(content.get("type")) == "text":
            return _clean_string(content.get("text"))
        return _clean_string(content.get("text"))
    return _clean_string(data.get("text"))


def _extract_response_text(response: Any) -> str:
    data = _to_plain(response)
    if isinstance(data, dict):
        for key in ("response_text", "responseText", "text"):
            value = _clean_string(data.get(key))
            if value:
                return value
        content = data.get("content")
        if isinstance(content, list):
            return "".join(_clean_string(item.get("text")) for item in content if isinstance(item, dict))
    return ""


def _session_id(session: Any) -> str:
    sid = _optional_session_id(session)
    if not sid:
        raise RuntimeError(f"ACP session response did not include a session id: {session!r}")
    return sid


def _optional_session_id(session: Any) -> str:
    return _get_value(session, "sessionId", "session_id", "id")


def _first_permission_option_id(options: Any) -> str:
    data = _to_plain(options)
    if isinstance(data, dict):
        raw_options = data.get("options")
    else:
        raw_options = data
    if isinstance(raw_options, list) and raw_options:
        first = raw_options[0]
        if isinstance(first, dict):
            return _clean_string(first.get("optionId") or first.get("option_id") or first.get("id"))
        return _clean_string(getattr(first, "optionId", "") or getattr(first, "option_id", "") or getattr(first, "id", ""))
    return ""


def _get_value(obj: Any, *keys: str) -> str:
    data = _to_plain(obj)
    if isinstance(data, dict):
        for key in keys:
            value = _clean_string(data.get(key))
            if value:
                return value
    for key in keys:
        value = _clean_string(getattr(obj, key, ""))
        if value:
            return value
    return ""


def _nested(data: dict[str, Any], *keys: str) -> str:
    cur: Any = data
    for key in keys:
        if not isinstance(cur, dict):
            return ""
        cur = cur.get(key)
    return _clean_string(cur)


def _to_plain(value: Any) -> Any:
    if is_dataclass(value):
        return asdict(value)
    if hasattr(value, "model_dump"):
        return value.model_dump()
    if hasattr(value, "dict"):
        return value.dict()
    return value


def _clean_string(value: Any) -> str:
    if value is None:
        return ""
    return str(value).strip()


def _print_response(payload: dict[str, Any]) -> None:
    print(json.dumps(payload, separators=(",", ":"), ensure_ascii=False))


if __name__ == "__main__":
    raise SystemExit(main())
