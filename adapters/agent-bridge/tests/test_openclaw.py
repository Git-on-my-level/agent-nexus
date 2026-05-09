from __future__ import annotations

import importlib.util
import json
import subprocess
from pathlib import Path

import pytest


_OPENCLAW_PATH = Path(__file__).resolve().parents[1] / "anx_agent_bridge" / "adapters" / "openclaw.py"
_SPEC = importlib.util.spec_from_file_location("openclaw", _OPENCLAW_PATH)
assert _SPEC is not None and _SPEC.loader is not None
openclaw = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(openclaw)


def test_openclaw_prompt_uses_absolute_anx_cli_for_context_fetch() -> None:
    prompt = openclaw._build_prompt(
        {
            "prompt_text": "Be concise.",
            "wake_packet": {
                "target": {"handle": "claw"},
                "workspace": {"id": "ws_main", "name": "Main"},
                "thread": {"id": "thread_1", "title": "Launch plan"},
                "trigger": {"message_event_id": "evt_1", "text": "@claw please review"},
                "reply_refs": ["message:evt_1"],
                "context_inline": {"current_summary": "The plan is waiting on review."},
            },
        },
        "/Users/example/.local/bin/anx",
    )

    assert "Agent Nexus wake request" in prompt
    assert "Launch plan (thread_1)" in prompt
    assert "@claw please review" in prompt
    assert "Reply refs: message:evt_1" in prompt
    assert "`/Users/example/.local/bin/anx threads workspace --thread-id thread_1 --json`" in prompt
    assert "Be concise." in prompt


def test_openclaw_extract_response_prefers_last_meaningful_payload() -> None:
    raw = json.dumps(
        {
            "result": {
                "payloads": [
                    {"text": "first visible text"},
                    {"payload": {"text": ""}},
                    {"text": "final visible text"},
                ]
            },
            "finalAssistantVisibleText": "fallback text",
        }
    )

    assert openclaw._extract_response_text(raw) == "final visible text"


def test_openclaw_extract_response_falls_back_to_final_visible_text() -> None:
    raw = json.dumps({"result": {"payloads": [{"text": ""}]}, "finalAssistantVisibleText": "final fallback"})

    assert openclaw._extract_response_text(raw) == "final fallback"


def test_openclaw_dispatch_strips_trailing_standalone_no_reply(monkeypatch) -> None:
    def fake_run(cmd: list[str], *, timeout: int):
        return subprocess.CompletedProcess(
            args=cmd,
            returncode=0,
            stdout=json.dumps({"result": {"payloads": [{"text": "Full answer.\n\nNO_REPLY"}]}}),
            stderr="",
        )

    monkeypatch.setattr(openclaw, "_resolve_bin", lambda settings, env_key, config_key, default_name: f"/bin/{default_name}")
    monkeypatch.setattr(openclaw, "_new_wake_session_id", lambda: "anx-abc123def456")
    monkeypatch.setattr(openclaw, "_run", fake_run)

    result = openclaw._dispatch(
        {
            "mode": "dispatch",
            "prompt_text": "wake prompt",
            "wake_packet": {"thread": {"id": "thread_1", "title": "Launch plan"}},
        },
        {},
    )

    assert result["response_text"] == "Full answer."


def test_openclaw_dispatch_keeps_inline_no_reply_content(monkeypatch) -> None:
    def fake_run(cmd: list[str], *, timeout: int):
        return subprocess.CompletedProcess(
            args=cmd,
            returncode=0,
            stdout=json.dumps({"result": {"payloads": [{"text": "Use NO_REPLY only as a sentinel."}]}}),
            stderr="",
        )

    monkeypatch.setattr(openclaw, "_resolve_bin", lambda settings, env_key, config_key, default_name: f"/bin/{default_name}")
    monkeypatch.setattr(openclaw, "_new_wake_session_id", lambda: "anx-abc123def456")
    monkeypatch.setattr(openclaw, "_run", fake_run)

    result = openclaw._dispatch(
        {
            "mode": "dispatch",
            "prompt_text": "wake prompt",
            "wake_packet": {"thread": {"id": "thread_1", "title": "Launch plan"}},
        },
        {},
    )

    assert result["response_text"] == "Use NO_REPLY only as a sentinel."


def test_openclaw_dispatch_creates_isolated_session_and_does_not_persist_native_session(monkeypatch) -> None:
    calls: list[list[str]] = []

    def fake_run(cmd: list[str], *, timeout: int):
        calls.append(cmd)
        return subprocess.CompletedProcess(
            args=cmd,
            returncode=0,
            stdout=json.dumps({"result": {"payloads": [{"text": "first"}, {"text": "final answer"}]}}),
            stderr="",
        )

    monkeypatch.setattr(openclaw, "_resolve_bin", lambda settings, env_key, config_key, default_name: f"/bin/{default_name}")
    monkeypatch.setattr(openclaw, "_new_wake_session_id", lambda: "anx-abc123def456")
    monkeypatch.setattr(openclaw, "_run", fake_run)

    result = openclaw._dispatch(
        {
            "mode": "dispatch",
            "prompt_text": "wake prompt",
            "existing_native_session_id": "shared-session-that-must-not-be-used",
            "wake_packet": {"thread": {"id": "thread_1", "title": "Launch plan"}},
        },
        {},
    )

    assert result["response_text"] == "final answer"
    assert result["native_session_id"] is None
    assert result["metadata"]["wake_session_id"] == "anx-abc123def456"
    assert calls[0][0:3] == ["/bin/openclaw", "agent", "--session-id"]
    assert calls[0][3] == "anx-abc123def456"
    assert "shared-session-that-must-not-be-used" not in calls[0]
    assert result["metadata"]["openclaw_command"][5] == "<prompt>"


def test_openclaw_dispatch_raises_on_launch_failure(monkeypatch) -> None:
    def fake_run(cmd: list[str], *, timeout: int):
        raise FileNotFoundError("missing openclaw")

    monkeypatch.setattr(openclaw, "_resolve_bin", lambda settings, env_key, config_key, default_name: f"/bin/{default_name}")
    monkeypatch.setattr(openclaw, "_new_wake_session_id", lambda: "anx-abc123def456")
    monkeypatch.setattr(openclaw, "_run", fake_run)

    with pytest.raises(RuntimeError, match="OpenClaw launch failed"):
        openclaw._dispatch(
            {
                "mode": "dispatch",
                "prompt_text": "wake prompt",
                "wake_packet": {"thread": {"id": "thread_1", "title": "Launch plan"}},
            },
            {},
        )


def test_openclaw_dispatch_raises_on_timeout(monkeypatch) -> None:
    def fake_run(cmd: list[str], *, timeout: int):
        raise subprocess.TimeoutExpired(cmd=cmd, timeout=timeout)

    monkeypatch.setattr(openclaw, "_resolve_bin", lambda settings, env_key, config_key, default_name: f"/bin/{default_name}")
    monkeypatch.setattr(openclaw, "_new_wake_session_id", lambda: "anx-abc123def456")
    monkeypatch.setattr(openclaw, "_run", fake_run)

    with pytest.raises(RuntimeError, match="Timed out waiting for OpenClaw agent response"):
        openclaw._dispatch(
            {
                "mode": "dispatch",
                "prompt_text": "wake prompt",
                "wake_packet": {"thread": {"id": "thread_1", "title": "Launch plan"}},
            },
            {"openclaw_timeout_seconds": 7},
        )


def test_openclaw_dispatch_raises_on_nonzero_exit(monkeypatch) -> None:
    def fake_run(cmd: list[str], *, timeout: int):
        return subprocess.CompletedProcess(args=cmd, returncode=2, stdout="", stderr="gateway unavailable")

    monkeypatch.setattr(openclaw, "_resolve_bin", lambda settings, env_key, config_key, default_name: f"/bin/{default_name}")
    monkeypatch.setattr(openclaw, "_new_wake_session_id", lambda: "anx-abc123def456")
    monkeypatch.setattr(openclaw, "_run", fake_run)

    with pytest.raises(RuntimeError, match="OpenClaw exited 2: gateway unavailable"):
        openclaw._dispatch(
            {
                "mode": "dispatch",
                "prompt_text": "wake prompt",
                "wake_packet": {"thread": {"id": "thread_1", "title": "Launch plan"}},
            },
            {},
        )


def test_openclaw_doctor_reports_gateway_status(monkeypatch) -> None:
    def fake_run(cmd: list[str], *, timeout: int):
        return subprocess.CompletedProcess(args=cmd, returncode=0, stdout="gateway ok\n", stderr="")

    monkeypatch.setattr(openclaw, "_resolve_bin", lambda settings, env_key, config_key, default_name: f"/bin/{default_name}")
    monkeypatch.setattr(openclaw, "_run", fake_run)

    result = openclaw._doctor({})

    assert result["schema_version"] == openclaw.RESPONSE_SCHEMA_VERSION
    assert result["ok"] is True
    assert result["details"]["gateway_status_rc"] == 0


def test_openclaw_doctor_returns_json_shape_when_gateway_probe_fails(monkeypatch) -> None:
    def fake_run(cmd: list[str], *, timeout: int):
        raise FileNotFoundError("missing openclaw")

    monkeypatch.setattr(openclaw, "_resolve_bin", lambda settings, env_key, config_key, default_name: f"/bin/{default_name}")
    monkeypatch.setattr(openclaw, "_run", fake_run)

    result = openclaw._doctor({})

    assert result["schema_version"] == openclaw.RESPONSE_SCHEMA_VERSION
    assert result["ok"] is False
    assert "gateway_status_error" in result["details"]
