from __future__ import annotations

import os
import importlib.util
from pathlib import Path


_HERMES_PATH = Path(__file__).resolve().parents[1] / "anx_agent_bridge" / "adapters" / "hermes_acp.py"
_SPEC = importlib.util.spec_from_file_location("hermes_acp", _HERMES_PATH)
assert _SPEC is not None and _SPEC.loader is not None
hermes_acp = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(hermes_acp)


def test_hermes_prompt_includes_wake_context() -> None:
    prompt = hermes_acp._build_prompt(
        {
            "prompt_text": "Original bridge prompt",
            "wake_packet": {
                "target": {"handle": "zara"},
                "workspace": {"id": "ws_main", "name": "Main"},
                "thread": {"id": "thread_1", "title": "Launch plan"},
                "trigger": {"message_event_id": "", "text": "@zara please review"},
                "context_inline": {"current_summary": "The plan is waiting on review."},
                "context_fetch": {
                    "preferred": "threads.workspace",
                    "cli": ["anx threads workspace --thread-id thread_1 --json"],
                },
            },
        }
    )

    assert "Agent Nexus wake request" in prompt
    assert "Launch plan (thread_1)" in prompt
    assert "Trigger event:" in prompt
    assert "@zara please review" in prompt
    assert "Original bridge prompt" in prompt


def test_hermes_session_update_text_collects_agent_chunks() -> None:
    update = {
        "sessionUpdate": "agent_message_chunk",
        "content": {"type": "text", "text": "hello"},
    }

    assert hermes_acp._session_update_text(update) == "hello"
    assert hermes_acp._session_update_text({"sessionUpdate": "tool_call"}) == ""
    assert (
        hermes_acp._session_update_text(
            {
                "sessionUpdate": "agent_message_chunk",
                "content": {"type": "tool_result", "text": "internal"},
            }
        )
        == ""
    )


def test_hermes_doctor_reports_missing_runtime(monkeypatch) -> None:
    monkeypatch.setenv("PATH", os.devnull)
    monkeypatch.delenv("HERMES_BIN", raising=False)

    result = hermes_acp._doctor({})

    assert result["schema_version"] == hermes_acp.RESPONSE_SCHEMA_VERSION
    assert result["ok"] is False
    assert result["details"]["hermes_bin"] == ""
    assert "Hermes binary not found" in result["message"]
