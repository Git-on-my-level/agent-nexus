from __future__ import annotations

from oar_agent_bridge.adapters.deterministic_ack import DeterministicAckAdapter
from oar_agent_bridge.models import WakePacket


def test_deterministic_ack_rotates_lines() -> None:
    adapter = DeterministicAckAdapter()
    p = WakePacket(
        wakeup_id="w1",
        handle="h",
        actor_id="a",
        workspace_id="ws",
        workspace_name="WS",
        thread_id="t1",
        thread_title="T",
        trigger_event_id="e1",
        trigger_created_at="2026-01-01T00:00:00Z",
        trigger_author_actor_id="a0",
        trigger_text="hi",
        current_summary="",
        session_key="sk",
        oar_base_url="http://127.0.0.1:8000",
        thread_context_url="",
        thread_workspace_url="",
        trigger_event_url="",
        cli_thread_inspect="",
        cli_thread_workspace="",
    )
    first = adapter.dispatch(p, "ping", "sess-a").response_text
    second = adapter.dispatch(p, "ping", "sess-b").response_text
    assert first != second
