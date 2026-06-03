"""Bundled OpenClaw subprocess adapter for ``anx-agent-bridge``.

OpenClaw gateway processes may not inherit the operator's interactive shell PATH, so this
adapter resolves absolute binary paths from config/env and passes those paths through the
wake prompt. Each wake uses a fresh OpenClaw ``--session-id`` by design: targeting a shared
or "main" session can deadlock when that session is already busy processing another turn.
"""

from __future__ import annotations

import json
import os
import shlex
import shutil
import subprocess
import sys
import uuid
from pathlib import Path
from typing import Any

DEFAULT_OPENCLAW_TIMEOUT_SECONDS = 300
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
            _print_response(_dispatch(request, settings))
            return 0
        raise ValueError(f"unsupported adapter mode: {mode!r}")
    except Exception as exc:
        print(f"openclaw adapter error: {exc}", file=sys.stderr)
        return 1


def _doctor(settings: dict[str, Any]) -> dict[str, Any]:
    details: dict[str, Any] = {}
    messages: list[str] = []
    ok = True

    openclaw_bin = _resolve_bin(settings, "OPENCLAW_BIN", "openclaw_bin", "openclaw")
    anx_cli_bin = _resolve_bin(settings, "ANX_CLI_BIN", "anx_cli_bin", "anx")
    details["openclaw_bin"] = openclaw_bin
    details["anx_cli_bin"] = anx_cli_bin
    details["timeout_seconds"] = _resolve_timeout(settings)

    if not openclaw_bin:
        ok = False
        messages.append("OpenClaw binary not found; set OPENCLAW_BIN or [adapter].openclaw_bin")
    if not anx_cli_bin:
        ok = False
        messages.append("anx CLI binary not found; set ANX_CLI_BIN or [adapter].anx_cli_bin")

    if openclaw_bin:
        try:
            proc = _run([openclaw_bin, "gateway", "status"], timeout=10)
            details["gateway_status_rc"] = proc.returncode
            details["gateway_status_stdout"] = proc.stdout[:500]
            details["gateway_status_stderr"] = proc.stderr[:500]
            if proc.returncode != 0:
                ok = False
                messages.append(f"OpenClaw gateway status failed with rc={proc.returncode}")
        except (OSError, subprocess.TimeoutExpired) as exc:
            ok = False
            details["gateway_status_error"] = str(exc)
            messages.append(f"OpenClaw gateway status probe failed: {exc}")

    return {
        "schema_version": RESPONSE_SCHEMA_VERSION,
        "ok": ok,
        "message": "; ".join(messages) if messages else "OpenClaw adapter is ready",
        "details": details,
    }


def _dispatch(request: dict[str, Any], settings: dict[str, Any]) -> dict[str, Any]:
    openclaw_bin = _resolve_bin(settings, "OPENCLAW_BIN", "openclaw_bin", "openclaw")
    if not openclaw_bin:
        raise RuntimeError("OpenClaw binary not found; set OPENCLAW_BIN or [adapter].openclaw_bin")
    anx_cli_bin = _resolve_bin(settings, "ANX_CLI_BIN", "anx_cli_bin", "anx")
    if not anx_cli_bin:
        raise RuntimeError("anx CLI binary not found; set ANX_CLI_BIN or [adapter].anx_cli_bin")

    timeout_seconds = _resolve_timeout(settings)
    wake_session_id = _new_wake_session_id()
    prompt = _build_prompt(request, anx_cli_bin)
    cmd = [
        openclaw_bin,
        "agent",
        "--session-id",
        wake_session_id,
        "--message",
        prompt,
        "--json",
        "--timeout",
        str(timeout_seconds),
    ]

    try:
        proc = _run(cmd, timeout=timeout_seconds + 10)
    except OSError as exc:
        raise RuntimeError(f"OpenClaw launch failed: {exc}") from exc
    except subprocess.TimeoutExpired as exc:
        raise RuntimeError(
            f"Timed out waiting for OpenClaw agent response after {timeout_seconds}s."
        ) from exc

    if proc.returncode != 0:
        err = (proc.stderr or proc.stdout or "unknown OpenClaw error").strip()
        raise RuntimeError(f"OpenClaw exited {proc.returncode}: {err[:500]}")

    response_text = _sanitize_response_text(_extract_response_text(proc.stdout))
    if not response_text:
        response_text = "OpenClaw completed the agent turn without returning text."
    return {
        "schema_version": RESPONSE_SCHEMA_VERSION,
        "response_text": response_text,
        # OpenClaw wake handling intentionally creates isolated per-wake sessions instead
        # of resuming a shared session, so there is no native session id to persist.
        "native_session_id": None,
        "metadata": {
            "adapter_kind": "openclaw",
            "wake_session_id": wake_session_id,
            "openclaw_command": _redact_prompt_from_command(cmd),
        },
    }


def _build_prompt(request: dict[str, Any], anx_cli_bin: str) -> str:
    packet = request.get("wake_packet") if isinstance(request.get("wake_packet"), dict) else {}
    prompt_text = _clean_string(request.get("prompt_text"))
    thread_id = _nested(packet, "thread", "id")
    thread_title = _nested(packet, "thread", "title")
    trigger_text = _nested(packet, "trigger", "text")
    reply_refs = packet.get("reply_refs", [])
    fetch_command = _thread_fetch_command(packet, anx_cli_bin)

    lines = [
        "Agent Nexus wake request",
        "",
        f"Handle: {_nested(packet, 'target', 'handle')}",
        f"Workspace: {_nested(packet, 'workspace', 'name')} ({_nested(packet, 'workspace', 'id')})",
        f"Thread: {thread_title} ({thread_id})",
        f"Trigger event: {_nested(packet, 'trigger', 'message_event_id')}",
        f"Trigger text: {trigger_text}",
    ]
    if isinstance(reply_refs, list) and reply_refs:
        lines.append(f"Reply refs: {', '.join(str(item) for item in reply_refs if str(item).strip())}")
    summary = _nested(packet, "context_inline", "current_summary")
    if summary:
        lines.extend(["", "Current summary:", summary])
    if fetch_command:
        lines.extend(["", "Context fetch:", f"Use `{fetch_command}` to read thread context."])
    if prompt_text:
        lines.extend(["", "Bridge prompt:", prompt_text])
    return "\n".join(lines).strip()


def _thread_fetch_command(packet: dict[str, Any], anx_cli_bin: str) -> str:
    thread_id = _nested(packet, "thread", "id")
    if not thread_id:
        return ""
    return shlex.join([anx_cli_bin, "threads", "workspace", "--thread-id", thread_id, "--json"])


def _extract_response_text(raw_stdout: str) -> str:
    raw = raw_stdout.strip()
    if not raw:
        return ""
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        return ""
    text = _last_payload_text(_lookup(data, "result", "payloads"))
    if text:
        return text
    text = _last_payload_text(_lookup(data, "payloads"))
    if text:
        return text
    for path in (
        ("finalAssistantVisibleText",),
        ("final_assistant_visible_text",),
        ("result", "finalAssistantVisibleText"),
        ("result", "final_assistant_visible_text"),
        ("result", "response_text"),
        ("response_text",),
        ("text",),
    ):
        text = _clean_string(_lookup(data, *path))
        if text:
            return text
    return ""


def _sanitize_response_text(text: str) -> str:
    lines = text.rstrip().splitlines()
    if lines and lines[-1].strip() == "NO_REPLY":
        lines = lines[:-1]
    return "\n".join(lines).strip()


def _last_payload_text(payloads: Any) -> str:
    if not isinstance(payloads, list):
        return ""
    for item in reversed(payloads):
        text = _payload_text(item)
        if text:
            return text
    return ""


def _payload_text(item: Any) -> str:
    if isinstance(item, dict):
        if not _payload_is_visible_final(item):
            return ""
        text = _clean_string(item.get("text"))
        if text:
            return text
        payload = item.get("payload")
        if isinstance(payload, dict):
            return _clean_string(payload.get("text"))
    return ""


def _payload_is_visible_final(item: dict[str, Any]) -> bool:
    markers = {
        _clean_string(item.get("type")).lower(),
        _clean_string(item.get("kind")).lower(),
        _clean_string(item.get("role")).lower(),
    }
    if markers & {"thought", "reasoning", "tool", "tool_call", "tool_result", "progress", "log"}:
        return False
    return bool(markers & {"assistant", "assistant_message", "final", "final_message", "final_visible_text"})


def _resolve_bin(settings: dict[str, Any], env_key: str, config_key: str, default_name: str) -> str:
    configured = _clean_string(os.environ.get(env_key)) or _clean_string(settings.get(config_key))
    if configured:
        return str(Path(configured).expanduser())
    return shutil.which(default_name) or ""


def _resolve_timeout(settings: dict[str, Any]) -> int:
    raw = os.environ.get("OPENCLAW_TIMEOUT_SECONDS") or settings.get("openclaw_timeout_seconds")
    try:
        return max(1, int(raw))
    except (TypeError, ValueError):
        return DEFAULT_OPENCLAW_TIMEOUT_SECONDS


def _new_wake_session_id() -> str:
    return f"anx-{uuid.uuid4().hex[:12]}"


def _run(cmd: list[str], *, timeout: int) -> subprocess.CompletedProcess[str]:
    return subprocess.run(cmd, check=False, capture_output=True, text=True, timeout=timeout)


def _redact_prompt_from_command(cmd: list[str]) -> list[str]:
    redacted = list(cmd)
    try:
        index = redacted.index("--message")
    except ValueError:
        return redacted
    if index + 1 < len(redacted):
        redacted[index + 1] = "<prompt>"
    return redacted


def _lookup(data: Any, *keys: str) -> Any:
    cur = data
    for key in keys:
        if not isinstance(cur, dict):
            return None
        cur = cur.get(key)
    return cur


def _nested(data: dict[str, Any], *keys: str) -> str:
    return _clean_string(_lookup(data, *keys))


def _clean_string(value: Any) -> str:
    if value is None:
        return ""
    return str(value).strip()


def _print_response(payload: dict[str, Any]) -> None:
    print(json.dumps(payload, separators=(",", ":"), ensure_ascii=False))


if __name__ == "__main__":
    raise SystemExit(main())
