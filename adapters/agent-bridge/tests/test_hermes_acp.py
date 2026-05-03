from __future__ import annotations

import asyncio
import os
import importlib.util
from pathlib import Path
from types import SimpleNamespace


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
    assert hermes_acp._session_update_text({"content": {"type": "text", "text": "missing kind"}}) == ""
    assert (
        hermes_acp._session_update_text(
            {
                "sessionUpdate": "agent_message_chunk",
                "content": {"type": "tool_result", "text": "internal"},
            }
        )
        == ""
    )


def test_hermes_client_ignores_internal_updates_and_collects_only_active_agent_messages() -> None:
    client = hermes_acp._HermesClient(allow_first_permission=False)
    thinking = (
        "(⌐■_■) formulating...(´･_･`) analyzing...(⊙_⊙) computing...\n\n"
        "Here's my read of your vault and top migration prescriptions:"
    )

    asyncio.run(
        client.session_update(
            "session-1",
            {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "old history"}},
        )
    )
    assert client.response_text() == ""

    client.start_response_collection()
    updates = [
        {"sessionUpdate": "agent_thought_chunk", "content": {"type": "text", "text": thinking}},
        {"sessionUpdate": "tool_call", "content": {"type": "text", "text": "internal tool text"}},
        {"sessionUpdate": "tool_call_update", "content": {"type": "text", "text": "internal result"}},
        {"sessionUpdate": "plan", "entries": []},
        {"sessionUpdate": "usage_update", "usage": {"totalTokens": 10}},
        {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "Here's my read"}},
        {"sessionUpdate": "agentMessageChunk", "content": {"type": "text", "text": " of your vault."}},
    ]
    for update in updates:
        asyncio.run(client.session_update("session-1", update))

    assert client.response_text() == "Here's my read of your vault."
    assert thinking not in client.response_text()


def test_hermes_response_fallback_used_only_without_streamed_chunks() -> None:
    client = hermes_acp._HermesClient(allow_first_permission=False)
    client.start_response_collection()

    assert client.has_response_chunks() is False
    assert hermes_acp._extract_response_text({"responseText": "fallback answer"}) == "fallback answer"

    asyncio.run(
        client.session_update(
            "session-1",
            {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "streamed answer"}},
        )
    )
    assert client.has_response_chunks() is True
    assert client.response_text() == "streamed answer"


def test_hermes_dispatch_clears_replayed_history_and_prefers_streamed_chunks(monkeypatch) -> None:
    class FakeACP:
        @staticmethod
        def text_block(text: str) -> dict[str, str]:
            return {"type": "text", "text": text}

    class FakeConnection:
        def __init__(self, client: hermes_acp._HermesClient) -> None:
            self.client = client

        async def initialize(self, **kwargs):
            return {}

        async def load_session(self, **kwargs):
            await self.client.session_update(
                "session-existing",
                {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "replayed history"}},
            )
            return {"sessionId": "session-existing"}

        async def prompt(self, **kwargs):
            await self.client.session_update(
                "session-existing",
                {
                    "sessionUpdate": "agent_thought_chunk",
                    "content": {"type": "text", "text": "(⊙_⊙) computing...\n\nnot visible"},
                },
            )
            await self.client.session_update(
                "session-existing",
                {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "new answer"}},
            )
            return {"responseText": "raw fallback should not win", "stopReason": "end_turn"}

    class FakeSpawn:
        def __init__(self, client: hermes_acp._HermesClient) -> None:
            self.client = client

        async def __aenter__(self):
            return FakeConnection(self.client), SimpleNamespace()

        async def __aexit__(self, exc_type, exc, tb):
            return False

    monkeypatch.setattr(hermes_acp, "_resolve_hermes_bin", lambda settings: "/bin/hermes")
    monkeypatch.setattr(hermes_acp, "_import_acp", lambda: FakeACP)
    monkeypatch.setattr(hermes_acp, "_spawn_agent_process", lambda acp_mod, client, cmd: FakeSpawn(client))
    monkeypatch.setattr(hermes_acp, "_resolve_cwd", lambda settings: "/workspace")

    result = asyncio.run(
        hermes_acp._dispatch(
            {
                "mode": "dispatch",
                "prompt_text": "wake prompt",
                "existing_native_session_id": "session-existing",
                "wake_packet": {},
            },
            {},
        )
    )

    assert result["response_text"] == "new answer"
    assert result["native_session_id"] == "session-existing"
    assert result["metadata"]["stop_reason"] == "end_turn"


def test_hermes_dispatch_uses_prompt_fallback_when_no_message_chunks(monkeypatch) -> None:
    class FakeACP:
        @staticmethod
        def text_block(text: str) -> dict[str, str]:
            return {"type": "text", "text": text}

    class FakeConnection:
        def __init__(self, client: hermes_acp._HermesClient) -> None:
            self.client = client

        async def initialize(self, **kwargs):
            return {}

        async def new_session(self, **kwargs):
            return {"sessionId": "session-new"}

        async def prompt(self, **kwargs):
            await self.client.session_update(
                "session-new",
                {"sessionUpdate": "agent_thought_chunk", "content": {"type": "text", "text": "hidden thought"}},
            )
            return {"responseText": "fallback answer", "stopReason": "end_turn"}

    class FakeSpawn:
        def __init__(self, client: hermes_acp._HermesClient) -> None:
            self.client = client

        async def __aenter__(self):
            return FakeConnection(self.client), SimpleNamespace()

        async def __aexit__(self, exc_type, exc, tb):
            return False

    monkeypatch.setattr(hermes_acp, "_resolve_hermes_bin", lambda settings: "/bin/hermes")
    monkeypatch.setattr(hermes_acp, "_import_acp", lambda: FakeACP)
    monkeypatch.setattr(hermes_acp, "_spawn_agent_process", lambda acp_mod, client, cmd: FakeSpawn(client))
    monkeypatch.setattr(hermes_acp, "_resolve_cwd", lambda settings: "/workspace")

    result = asyncio.run(hermes_acp._dispatch({"mode": "dispatch", "prompt_text": "wake prompt", "wake_packet": {}}, {}))

    assert result["response_text"] == "fallback answer"


def test_hermes_doctor_reports_missing_runtime(monkeypatch) -> None:
    monkeypatch.setenv("PATH", os.devnull)
    monkeypatch.delenv("HERMES_BIN", raising=False)

    result = hermes_acp._doctor({})

    assert result["schema_version"] == hermes_acp.RESPONSE_SCHEMA_VERSION
    assert result["ok"] is False
    assert result["details"]["hermes_bin"] == ""
    assert "Hermes binary not found" in result["message"]
