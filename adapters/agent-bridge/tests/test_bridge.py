import pytest

from pathlib import Path

from oar_agent_bridge.bridge import AgentBridge
from oar_agent_bridge.config import AdapterConfig, AgentConfig, LoadedConfig, OARConfig
from oar_agent_bridge.oar_client import OARClientError


class StubState:
    def __init__(self):
        self.last_event_id = None
        self.bridge_instance_id = "bridge-test"
        self._handled = set()

    def handled_wakeup_ids(self):
        return self._handled

    def mark_wakeup_handled(self, wakeup_id: str):
        self._handled.add(wakeup_id)

    def session_map(self):
        return {}


class StubClient:
    def __init__(self, events):
        self._events = list(events)

    def stream_events(self, **_kwargs):
        for event in self._events:
            yield event
        raise KeyboardInterrupt()


class StubAdapter:
    pass


def build_bridge(events):
    config = LoadedConfig(
        oar=OARConfig(base_url="http://oar.test", workspace_id="ws_main", workspace_name="Main"),
        agent=AgentConfig(
            handle="hermes",
            driver_kind="custom",
            adapter_kind="hermes_acp",
            state_dir=Path("/tmp/oar-agent-bridge-test"),
        ),
        router=None,
        adapter=AdapterConfig(raw={}),
        auth_state_path=Path("/tmp/oar-agent-bridge-test-auth.json"),
    )
    state = StubState()
    bridge = AgentBridge(config, StubClient(events), state, StubAdapter())
    return bridge, state


def test_bridge_advances_cursor_for_non_target_event():
    bridge, state = build_bridge(
        [{"data": '{"event":{"id":"evt-1","payload":{"target_handle":"other","wakeup_id":"wake-1"}}}'}]
    )

    try:
        bridge.run_forever()
    except KeyboardInterrupt:
        pass

    assert state.last_event_id == "evt-1"


def test_bridge_does_not_advance_cursor_when_handle_fails():
    bridge, state = build_bridge(
        [{"data": '{"event":{"id":"evt-2","payload":{"target_handle":"hermes","wakeup_id":"wake-2"}}}'}]
    )

    def fail(_event):
        raise KeyboardInterrupt()

    bridge._handle_wakeup = fail

    try:
        bridge.run_forever()
    except KeyboardInterrupt:
        pass

    assert state.last_event_id is None


def test_claim_wakeup_returns_false_on_conflict():
    bridge, _state = build_bridge([])

    def raise_conflict(**_kwargs):
        raise OARClientError(409, "conflict", "duplicate request key")

    bridge.client.create_event = raise_conflict

    assert bridge._claim_wakeup("wake-1", "thread-1", "actor-1", "event-1") is False
