import logging
import pytest

from pathlib import Path
from types import SimpleNamespace

from anx_agent_bridge.bridge import AgentBridge
from anx_agent_bridge.config import AdapterConfig, AgentConfig, LoadedConfig, ANXConfig
from anx_agent_bridge.models import WakePacket
from anx_agent_bridge.anx_client import ANXClientError, ANXStreamDisconnected
from anx_agent_bridge.util import generate_bridge_proof_keypair


class StubState:
    def __init__(self):
        public_key_b64, private_key_b64 = generate_bridge_proof_keypair()
        self.last_event_id = None
        self.bridge_instance_id = "bridge-test"
        self.bridge_signing_public_key_spki_b64 = public_key_b64
        self.bridge_signing_private_key_pkcs8_b64 = private_key_b64
        self._handled = set()
        self._sessions = {}

    def handled_wakeup_ids(self):
        return self._handled

    def mark_wakeup_handled(self, wakeup_id: str):
        self._handled.add(wakeup_id)

    def session_map(self):
        return dict(self._sessions)

    def set_session(self, session_key: str, session_id: str):
        self._sessions[session_key] = session_id


class StubClient:
    def __init__(self, events):
        self._events = list(events)
        self.registration_updates = []
        self.created_events = []
        self.list_notification_calls = []
        self.notification_reads = []
        self.notifications = []

    def stream_events(self, **_kwargs):
        for event in self._events:
            yield event
        raise KeyboardInterrupt()

    def patch_current_agent(self, **kwargs):
        self.registration_updates.append(kwargs)
        return {"agent": {"agent_id": "agent-hermes", "registration": kwargs.get("registration")}}

    def create_event(self, **kwargs):
        self.created_events.append(kwargs)
        return {"event": {"id": f"event-{len(self.created_events)}", **kwargs.get("event", {})}}

    def list_agent_notifications(self, *, statuses=None, order="desc"):
        self.list_notification_calls.append({"statuses": list(statuses or []), "order": order})
        return list(self.notifications)

    def mark_agent_notification_read(self, wakeup_id):
        self.notification_reads.append(wakeup_id)
        return {"notification": {"wakeup_id": wakeup_id, "status": "read"}}

    def get_artifact_content(self, _artifact_id):
        return {
            "wakeup_id": "wake-1",
            "target": {"handle": "hermes", "actor_id": "actor-hermes"},
            "workspace": {"id": "ws_main", "name": "Main"},
            "thread": {"id": "thread-1", "title": "Thread"},
            "subject_ref": "topic:topic-1",
            "resolved_subject": {
                "ref": "topic:topic-1",
                "kind": "topic",
                "title": "Topic",
                "thread_id": "thread-1",
            },
            "trigger": {
                "message_event_id": "evt-trigger",
                "created_at": "2026-03-29T00:00:00Z",
                "author_actor_id": "actor-human",
                "text": "@hermes summarize",
            },
            "context_inline": {"current_summary": "summary"},
            "session_key": "thread:thread-1",
            "context_fetch": {
                "cli": ["anx threads workspace --thread-id thread-1", "anx threads inspect --thread-id thread-1"],
                "api": {
                    "thread": "http://anx.test/threads/thread-1",
                    "context": "http://anx.test/threads/thread-1/context",
                    "workspace": "http://anx.test/threads/thread-1/workspace",
                    "trigger_event": "http://anx.test/events/evt-trigger",
                },
            },
        }

    def get_current_agent(self):
        return {"agent": {"agent_id": "agent-hermes"}}


class StubAdapter:
    def __init__(self):
        self.dispatch_calls = []
        self.last_packet = None
        self.last_prompt_text = ""

    def doctor(self):
        return {"adapter_kind": "stub"}

    def dispatch(self, packet, _prompt_text, _session_key, existing_native_session_id=None):
        self.dispatch_calls.append(
            {
                "packet": packet,
                "prompt_text": _prompt_text,
                "session_key": _session_key,
                "existing_native_session_id": existing_native_session_id,
            }
        )
        self.last_packet = packet
        self.last_prompt_text = _prompt_text
        return SimpleNamespace(response_text="done", native_session_id=existing_native_session_id or "native-1")


class StubAuthState:
    username = "hermes"
    agent_id = "agent-hermes"
    actor_id = "actor-hermes"


class StubAuth:
    def require_state(self):
        return StubAuthState()


def build_bridge(events):
    config = LoadedConfig(
        anx=ANXConfig(base_url="http://anx.test", workspace_id="ws_main", workspace_name="Main"),
        agent=AgentConfig(
            handle="hermes",
            driver_kind="custom",
            adapter_kind="hermes_acp",
            state_dir=Path("/tmp/anx-agent-bridge-test"),
            workspace_bindings=["ws_main"],
        ),
        adapter=AdapterConfig(raw={}),
        auth_state_path=Path("/tmp/anx-agent-bridge-test-auth.json"),
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

    def fail():
        raise KeyboardInterrupt()

    bridge._drain_notifications = fail

    try:
        bridge.run_forever()
    except KeyboardInterrupt:
        pass

    assert state.last_event_id is None


def test_claim_wakeup_returns_false_on_conflict():
    bridge, _state, _client = build_bridge([])

    def raise_conflict(**_kwargs):
        raise ANXClientError(409, "conflict", "duplicate request key")

    bridge.client.create_event = raise_conflict

    packet = WakePacket.from_content(bridge.client.get_artifact_content("wake-1"))
    assert bridge._claim_wakeup(packet, "actor-1", "event-1") is False


def test_bridge_logs_transport_disconnect_without_traceback(monkeypatch, caplog):
    bridge, state, _client = build_bridge([])
    caplog.set_level(logging.INFO)

    monkeypatch.setattr(bridge, "_start_checkin_loop", lambda: None)

    def raise_disconnect(**_kwargs):
        raise ANXStreamDisconnected("incomplete chunked read")

    def stop_sleep(_seconds):
        raise KeyboardInterrupt()

    bridge.client.stream_events = raise_disconnect
    monkeypatch.setattr("anx_agent_bridge.bridge.time.sleep", stop_sleep)

    with pytest.raises(KeyboardInterrupt):
        bridge.run_forever()

    assert state.last_event_id is None
    assert "Event stream interrupted; reconnecting" in caplog.text
    assert "Bridge loop failed; reconnecting" not in caplog.text


def test_bridge_retries_when_startup_notification_drain_fails(monkeypatch, caplog):
    bridge, _state, _client = build_bridge([])
    caplog.set_level(logging.INFO)
    calls = {"drain": 0}

    monkeypatch.setattr(bridge, "_start_checkin_loop", lambda: None)

    def flaky_drain():
        calls["drain"] += 1
        if calls["drain"] == 1:
            raise RuntimeError("temporary drain failure")
        raise KeyboardInterrupt()

    monkeypatch.setattr(bridge, "_drain_notifications", flaky_drain)
    monkeypatch.setattr("anx_agent_bridge.bridge.time.sleep", lambda _seconds: None)

    with pytest.raises(KeyboardInterrupt):
        bridge.run_forever()

    assert calls["drain"] == 2
    assert "Bridge loop failed; reconnecting" in caplog.text


def test_handle_notification_marks_read_after_dispatch():
    bridge, state, client = build_bridge([])

    bridge._handle_notification(
        {
            "wakeup_id": "wake-1",
            "target_actor_id": "actor-hermes",
            "thread_id": "thread-1",
            "request_event_id": "evt-request",
            "trigger_event_id": "evt-trigger",
        }
    )

    assert client.notification_reads == ["wake-1"]
    assert "wake-1" in state.handled_wakeup_ids()
    assert bridge.adapter.last_prompt_text.startswith(
        "You were tagged in an Agent Nexus topic or card."
    )
    assert '"subject_ref": "topic:topic-1"' in bridge.adapter.last_prompt_text
    assert '"resolved_subject"' in bridge.adapter.last_prompt_text
    assert [entry["event"]["type"] for entry in client.created_events] == [
        "agent_wakeup_claimed",
        "message_posted",
        "agent_wakeup_completed",
    ]
    assert client.created_events[0]["event"]["refs"] == [
        "thread:thread-1",
        "topic:topic-1",
        "event:evt-request",
        "artifact:wake-1",
    ]
    assert client.created_events[1]["event"]["refs"] == [
        "thread:thread-1",
        "topic:topic-1",
        "event:evt-trigger",
        "artifact:wake-1",
    ]
    assert client.created_events[2]["event"]["refs"] == [
        "thread:thread-1",
        "topic:topic-1",
        "event:evt-trigger",
        "artifact:wake-1",
    ]


def test_handle_notification_leaves_notification_unread_when_completion_fails():
    bridge, _state, client = build_bridge([])
    original_create_event = client.create_event

    def fail_completion(**kwargs):
        event = kwargs.get("event") or {}
        if event.get("type") == "agent_wakeup_completed":
            raise RuntimeError("completion write failed")
        return original_create_event(**kwargs)

    client.create_event = fail_completion

    with pytest.raises(RuntimeError, match="completion write failed"):
        bridge._handle_notification(
            {
                "wakeup_id": "wake-1",
                "target_actor_id": "actor-hermes",
                "thread_id": "thread-1",
                "request_event_id": "evt-request",
                "trigger_event_id": "evt-trigger",
            }
        )

    assert client.notification_reads == []


def test_handle_notification_does_not_emit_failed_when_read_ack_fails(monkeypatch):
    bridge, _state, client = build_bridge([])
    failures = {"count": 0}

    def fail_mark_read(_wakeup_id):
        failures["count"] += 1
        raise RuntimeError("read ack failed")

    client.mark_agent_notification_read = fail_mark_read
    monkeypatch.setattr("anx_agent_bridge.bridge.time.sleep", lambda _seconds: None)

    bridge._handle_notification(
        {
            "wakeup_id": "wake-1",
            "target_actor_id": "actor-hermes",
            "thread_id": "thread-1",
            "request_event_id": "evt-request",
            "trigger_event_id": "evt-trigger",
        }
    )

    assert failures["count"] == 3
    event_types = [entry["event"]["type"] for entry in client.created_events]
    assert "agent_wakeup_completed" in event_types
    assert "agent_wakeup_failed" not in event_types


def test_handle_notification_skips_redispatch_for_handled_wakeup():
    bridge, state, client = build_bridge([])
    state.mark_wakeup_handled("wake-1")
    dispatch_calls = {"count": 0}

    def fail_dispatch(*_args, **_kwargs):
        dispatch_calls["count"] += 1
        raise AssertionError("handled wakeup should not dispatch again")

    bridge.adapter.dispatch = fail_dispatch

    bridge._handle_notification(
        {
            "wakeup_id": "wake-1",
            "status": "unread",
            "target_actor_id": "actor-hermes",
            "thread_id": "thread-1",
            "request_event_id": "evt-request",
            "trigger_event_id": "evt-trigger",
        }
    )

    assert dispatch_calls["count"] == 0
    assert client.notification_reads == ["wake-1"]
    assert client.created_events == []


def test_drain_notifications_includes_read_status():
    bridge, _state, client = build_bridge([])

    def noop_handle(_notification):
        return None

    bridge._handle_notification = noop_handle
    client.notifications = [{"wakeup_id": "wake-1", "status": "read", "thread_id": "thread-1"}]

    bridge._drain_notifications()

    assert client.list_notification_calls == [{"statuses": ["unread", "read"], "order": "asc"}]


def test_bridge_checkin_upserts_active_registration():
    bridge, _state, client = build_bridge([])

    bridge._publish_checkin()

    assert len(client.registration_updates) == 1
    reg_payload = client.registration_updates[0]["registration"]
    assert reg_payload["status"] == "active"
    assert reg_payload["bridge_instance_id"] == "bridge-test"
    assert reg_payload["bridge_signing_public_key_spki_b64"] != ""
    assert reg_payload["bridge_checked_in_at"] != ""
    assert reg_payload["bridge_expires_at"] != ""
    assert reg_payload["bridge_checkin_event_id"] == "event-1"
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

    assert client.registration_updates == []
    assert client.created_events == []
