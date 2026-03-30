import logging
import pytest

from pathlib import Path

from oar_agent_bridge.bridge import AgentBridge
from oar_agent_bridge.config import AdapterConfig, AgentConfig, LoadedConfig, OARConfig
from oar_agent_bridge.oar_client import OARClientError, OARStreamDisconnected
from oar_agent_bridge.util import generate_bridge_proof_keypair


class StubState:
    def __init__(self):
        public_key_b64, private_key_b64 = generate_bridge_proof_keypair()
        self.last_event_id = None
        self.bridge_instance_id = "bridge-test"
        self.bridge_signing_public_key_spki_b64 = public_key_b64
        self.bridge_signing_private_key_pkcs8_b64 = private_key_b64
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
        self.upserts = []
        self.created_events = []

    def stream_events(self, **_kwargs):
        for event in self._events:
            yield event
        raise KeyboardInterrupt()

    def upsert_document(self, document_id, **kwargs):
        self.upserts.append((document_id, kwargs))
        return {"document_id": document_id}

    def create_event(self, **kwargs):
        self.created_events.append(kwargs)
        return {"event": {"id": f"event-{len(self.created_events)}", **kwargs.get("event", {})}}

    def get_document(self, _document_id):
        raise OARClientError(404, "not_found", "missing")


class StubAdapter:
    def doctor(self):
        return {"adapter_kind": "stub"}


class StubAuthState:
    username = "hermes"
    actor_id = "actor-hermes"


class StubAuth:
    def require_state(self):
        return StubAuthState()


def build_bridge(events):
    config = LoadedConfig(
        oar=OARConfig(base_url="http://oar.test", workspace_id="ws_main", workspace_name="Main"),
        agent=AgentConfig(
            handle="hermes",
            driver_kind="custom",
            adapter_kind="hermes_acp",
            state_dir=Path("/tmp/oar-agent-bridge-test"),
            workspace_bindings=["ws_main"],
        ),
        adapter=AdapterConfig(raw={}),
        auth_state_path=Path("/tmp/oar-agent-bridge-test-auth.json"),
    )
    state = StubState()
    client = StubClient(events)
    bridge = AgentBridge(config, StubAuth(), client, state, StubAdapter())
    return bridge, state, client


def test_bridge_advances_cursor_for_non_target_event():
    bridge, state, _client = build_bridge(
        [{"data": '{"event":{"id":"evt-1","payload":{"target_handle":"other","wakeup_id":"wake-1"}}}'}]
    )

    try:
        bridge.run_forever()
    except KeyboardInterrupt:
        pass

    assert state.last_event_id == "evt-1"


def test_bridge_does_not_advance_cursor_when_handle_fails():
    bridge, state, _client = build_bridge(
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
    bridge, _state, _client = build_bridge([])

    def raise_conflict(**_kwargs):
        raise OARClientError(409, "conflict", "duplicate request key")

    bridge.client.create_event = raise_conflict

    assert bridge._claim_wakeup("wake-1", "thread-1", "actor-1", "event-1") is False


def test_bridge_logs_transport_disconnect_without_traceback(monkeypatch, caplog):
    bridge, state, _client = build_bridge([])
    caplog.set_level(logging.INFO)

    monkeypatch.setattr(bridge, "_start_checkin_loop", lambda: None)

    def raise_disconnect(**_kwargs):
        raise OARStreamDisconnected("incomplete chunked read")

    def stop_sleep(_seconds):
        raise KeyboardInterrupt()

    bridge.client.stream_events = raise_disconnect
    monkeypatch.setattr("oar_agent_bridge.bridge.time.sleep", stop_sleep)

    with pytest.raises(KeyboardInterrupt):
        bridge.run_forever()

    assert state.last_event_id is None
    assert "Event stream interrupted; reconnecting" in caplog.text
    assert "Bridge loop failed; reconnecting" not in caplog.text


def test_bridge_checkin_upserts_active_registration():
    bridge, _state, client = build_bridge([])

    bridge._publish_checkin()

    assert len(client.upserts) == 1
    reg_payload = client.upserts[0][1]
    assert reg_payload["document"]["status"] == "active"
    assert reg_payload["content"]["status"] == "active"
    assert reg_payload["content"]["bridge_instance_id"] == "bridge-test"
    assert reg_payload["content"]["bridge_signing_public_key_spki_b64"] != ""
    assert reg_payload["content"]["bridge_checked_in_at"] != ""
    assert reg_payload["content"]["bridge_expires_at"] != ""
    assert reg_payload["content"]["bridge_checkin_event_id"] == "event-1"
    assert len(client.created_events) == 1
    checkin_event = client.created_events[0]["event"]
    checkin_payload = checkin_event["payload"]
    assert checkin_event["type"] == "agent_bridge_checked_in"
    assert checkin_event["refs"] == []
    assert checkin_event["provenance"] == {"sources": ["inferred"]}
    assert checkin_payload["bridge_instance_id"] == "bridge-test"
    assert checkin_payload["workspace_id"] == "ws_main"
    assert checkin_payload["proof_signature_b64"] != ""


def test_bridge_checkin_requires_adapter_doctor_to_pass():
    bridge, _state, client = build_bridge([])

    class BrokenAdapter:
        def doctor(self):
            raise RuntimeError("adapter not ready")

    bridge.adapter = BrokenAdapter()

    with pytest.raises(RuntimeError, match="adapter not ready"):
        bridge._publish_checkin()

    assert client.upserts == []
    assert client.created_events == []
