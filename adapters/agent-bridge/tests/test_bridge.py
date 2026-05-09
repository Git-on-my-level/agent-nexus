import logging
import sys
import pytest

from pathlib import Path
from types import SimpleNamespace

from anx_agent_bridge.bridge import AgentBridge
from anx_agent_bridge.adapters.subprocess_adapter import SubprocessAdapter
from anx_agent_bridge.config import AdapterConfig, AgentConfig, LoadedConfig, ANXConfig, WorkspaceConfig
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
        self._completion_pending = set()
        self._sessions = {}

    def handled_wakeup_ids(self):
        return self._handled

    def mark_wakeup_handled(self, wakeup_id: str):
        self._handled.add(wakeup_id)

    def completion_pending_wakeup_ids(self):
        return self._completion_pending

    def mark_wakeup_completion_pending(self, wakeup_id: str):
        self._completion_pending.add(wakeup_id)

    def clear_wakeup_completion_pending(self, wakeup_id: str):
        self._completion_pending.discard(wakeup_id)

    def session_map(self):
        return dict(self._sessions)

    def set_session(self, session_key: str, session_id: str):
        self._sessions[session_key] = session_id


class StubClient:
    def __init__(self, events):
        self._events = list(events)
        self.registration_updates = []
        self.bridge_checkins = []
        self.created_events = []
        self.claimed_wakeups = []
        self.completed_wakeups = []
        self.failed_wakeups = []
        self.list_notification_calls = []
        self.notification_reads = []
        self.notifications = []

    def stream_events(self, **_kwargs):
        for event in self._events:
            yield event
        raise KeyboardInterrupt()

    def stream_agent_notifications(self, **_kwargs):
        for event in self._events:
            yield event
        raise KeyboardInterrupt()

    def patch_current_agent(self, **kwargs):
        self.registration_updates.append(kwargs)
        return {"agent": {"agent_id": "agent-hermes", "registration": kwargs.get("registration")}}

    def create_event(self, **kwargs):
        self.created_events.append(kwargs)
        return {"event": {"id": f"event-{len(self.created_events)}", **kwargs.get("event", {})}}

    def bridge_check_in(self, payload):
        self.bridge_checkins.append(payload)
        return {"agent": {"agent_id": "agent-hermes", "registration": payload}}

    def claim_agent_wakeup(self, wakeup_id, bridge_instance_id):
        self.claimed_wakeups.append({"wakeup_id": wakeup_id, "bridge_instance_id": bridge_instance_id})
        return {"notification": {"wakeup_id": wakeup_id, "bridge_instance_id": bridge_instance_id}}

    def complete_agent_wakeup(self, wakeup_id, bridge_instance_id):
        self.completed_wakeups.append({"wakeup_id": wakeup_id, "bridge_instance_id": bridge_instance_id})
        return {"notification": {"wakeup_id": wakeup_id, "bridge_instance_id": bridge_instance_id, "delivery_status": "completed"}}

    def fail_agent_wakeup(self, wakeup_id, bridge_instance_id, error):
        self.failed_wakeups.append({"wakeup_id": wakeup_id, "bridge_instance_id": bridge_instance_id, "error": error})
        return {"notification": {"wakeup_id": wakeup_id, "bridge_instance_id": bridge_instance_id, "delivery_status": "failed"}}

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


def build_bridge(events, *, workspace_ids=None):
    workspace_ids = workspace_ids or ["ws_main"]
    config = LoadedConfig(
        anx=ANXConfig(base_url="http://anx.test", workspace_id=workspace_ids[0], workspace_name="Main"),
        agent=AgentConfig(
            handle="hermes",
            driver_kind="custom",
            adapter_kind="subprocess",
            state_dir=Path("/tmp/anx-agent-bridge-test"),
            workspace_bindings=workspace_ids,
        ),
        adapter=AdapterConfig(raw={}),
        auth_state_path=Path("/tmp/anx-agent-bridge-test-auth.json"),
        workspaces=[
            WorkspaceConfig(id=workspace_id, name=workspace_id, base_url="http://anx.test")
            for workspace_id in workspace_ids
        ],
    )
    state = StubState()
    client = StubClient(events)
    bridge = AgentBridge(config, StubAuth(), client, state, StubAdapter())
    return bridge, state, client


def test_claim_wakeup_returns_false_on_conflict():
    bridge, _state, _client = build_bridge([])

    def raise_conflict(*_args, **_kwargs):
        raise ANXClientError(409, "conflict", "duplicate request key")

    bridge.client.claim_agent_wakeup = raise_conflict

    packet = WakePacket.from_content(bridge.client.get_artifact_content("wake-1"))
    assert bridge._claim_wakeup(packet, "actor-1", "event-1") is False


def test_bridge_retries_when_notification_poll_raises(monkeypatch, caplog):
    bridge, state, _client = build_bridge([])
    caplog.set_level(logging.INFO)

    monkeypatch.setattr(bridge, "_start_checkin_loop", lambda: None)

    def raise_disconnect(**_kwargs):
        raise ANXStreamDisconnected("incomplete chunked read")

    def stop_sleep(_seconds):
        raise KeyboardInterrupt()

    bridge.client.stream_agent_notifications = raise_disconnect
    monkeypatch.setattr("anx_agent_bridge.bridge.time.sleep", stop_sleep)

    with pytest.raises(KeyboardInterrupt):
        bridge.run_forever()

    assert state.last_event_id is None
    assert "Bridge loop failed; reconnecting" in caplog.text


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
    assert client.claimed_wakeups == [{"wakeup_id": "wake-1", "bridge_instance_id": "bridge-test"}]
    assert client.completed_wakeups == [{"wakeup_id": "wake-1", "bridge_instance_id": "bridge-test"}]
    assert [entry["event"]["type"] for entry in client.created_events] == ["message_posted"]
    assert client.created_events[0]["event"]["refs"] == [
        "thread:thread-1",
        "topic:topic-1",
        "event:evt-trigger",
        "artifact:wake-1",
    ]


def test_packet_event_refs_omits_empty_trigger_event_id():
    bridge, _state, client = build_bridge([])
    packet_content = client.get_artifact_content("wake-1")
    packet_content["trigger"]["message_event_id"] = ""
    packet = WakePacket.from_content(packet_content)

    assert bridge._packet_event_refs(packet, packet.trigger_event_id) == [
        "thread:thread-1",
        "topic:topic-1",
        "artifact:wake-1",
    ]


def test_handle_notification_retries_completion_without_redispatch_after_reply_post():
    bridge, state, client = build_bridge([])
    completion_attempts = {"count": 0}

    def fail_completion(*_args, **_kwargs):
        completion_attempts["count"] += 1
        if completion_attempts["count"] == 1:
            raise RuntimeError("completion write failed")
        return {
            "notification": {
                "wakeup_id": "wake-1",
                "bridge_instance_id": "bridge-test",
                "delivery_status": "completed",
            }
        }

    client.complete_agent_wakeup = fail_completion
    notification = {
        "wakeup_id": "wake-1",
        "target_actor_id": "actor-hermes",
        "thread_id": "thread-1",
        "request_event_id": "evt-request",
        "trigger_event_id": "evt-trigger",
    }

    bridge._handle_notification(notification)

    assert client.notification_reads == ["wake-1"]
    assert "wake-1" in state.handled_wakeup_ids()
    assert "wake-1" in state.completion_pending_wakeup_ids()
    assert client.failed_wakeups[-1]["wakeup_id"] == "wake-1"
    assert "completion write failed" in client.failed_wakeups[-1]["error"]
    assert len(bridge.adapter.dispatch_calls) == 1
    assert [entry["event"]["type"] for entry in client.created_events] == ["message_posted"]

    bridge._handle_notification(notification)

    assert completion_attempts["count"] == 2
    assert len(bridge.adapter.dispatch_calls) == 1
    assert "wake-1" in state.handled_wakeup_ids()
    assert "wake-1" not in state.completion_pending_wakeup_ids()
    assert client.notification_reads == ["wake-1", "wake-1"]
    assert [entry["event"]["type"] for entry in client.created_events] == ["message_posted"]


def test_handle_notification_retries_reply_post_without_redispatch(monkeypatch):
    bridge, state, client = build_bridge([])
    original_create_event = client.create_event
    attempts = {"message": 0}

    def flaky_message_post(**kwargs):
        event = kwargs.get("event") or {}
        if event.get("type") == "message_posted":
            attempts["message"] += 1
            if attempts["message"] < 3:
                raise RuntimeError("temporary post failure")
        return original_create_event(**kwargs)

    client.create_event = flaky_message_post
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

    assert attempts["message"] == 3
    assert len(bridge.adapter.dispatch_calls) == 1
    assert client.notification_reads == ["wake-1"]
    assert "wake-1" in state.handled_wakeup_ids()
    event_types = [entry["event"]["type"] for entry in client.created_events]
    assert event_types == ["message_posted"]
    assert client.completed_wakeups == [{"wakeup_id": "wake-1", "bridge_instance_id": "bridge-test"}]


def test_handle_notification_does_not_mark_consumed_after_reply_post_exhausts_retries(monkeypatch):
    bridge, state, client = build_bridge([])
    original_create_event = client.create_event
    attempts = {"message": 0}

    def fail_message_post(**kwargs):
        event = kwargs.get("event") or {}
        if event.get("type") == "message_posted":
            attempts["message"] += 1
            raise RuntimeError("post rejected")
        return original_create_event(**kwargs)

    client.create_event = fail_message_post
    monkeypatch.setattr("anx_agent_bridge.bridge.time.sleep", lambda _seconds: None)
    notification = {
        "wakeup_id": "wake-1",
        "target_actor_id": "actor-hermes",
        "thread_id": "thread-1",
        "request_event_id": "evt-request",
        "trigger_event_id": "evt-trigger",
    }

    bridge._handle_notification(notification)
    bridge._handle_notification(notification)

    assert attempts["message"] == 6
    assert len(bridge.adapter.dispatch_calls) == 2
    assert client.notification_reads == ["wake-1", "wake-1"]
    assert "wake-1" not in state.handled_wakeup_ids()
    assert [entry["event"]["type"] for entry in client.created_events] == []
    assert client.failed_wakeups[-1]["wakeup_id"] == "wake-1"


def test_handle_notification_caps_dispatch_failures(monkeypatch):
    bridge, state, client = build_bridge([])
    now = {"value": 0.0}
    calls = {"dispatch": 0}

    def fail_dispatch(*_args, **_kwargs):
        calls["dispatch"] += 1
        raise RuntimeError("adapter down")

    bridge.adapter.dispatch = fail_dispatch
    monkeypatch.setattr("anx_agent_bridge.bridge.time.monotonic", lambda: now["value"])
    monkeypatch.setattr("anx_agent_bridge.bridge.time.sleep", lambda _seconds: None)
    notification = {
        "wakeup_id": "wake-1",
        "target_actor_id": "actor-hermes",
        "thread_id": "thread-1",
        "request_event_id": "evt-request",
        "trigger_event_id": "evt-trigger",
    }

    bridge._handle_notification(notification)
    now["value"] = 61
    bridge._handle_notification(notification)
    now["value"] = 182
    bridge._handle_notification(notification)
    now["value"] = 423
    bridge._handle_notification(notification)

    assert calls["dispatch"] == 3
    assert "wake-1" in state.handled_wakeup_ids()
    assert client.notification_reads == ["wake-1"]
    assert len(client.failed_wakeups) == 3


def test_drain_notifications_does_not_block_on_backed_off_wakeup(monkeypatch):
    bridge, _state, client = build_bridge([])
    now = {"value": 100.0}
    base_artifact = client.get_artifact_content("wake-1")

    def artifact_for(wakeup_id: str):
        return {**base_artifact, "wakeup_id": wakeup_id}

    client.get_artifact_content = lambda wid: artifact_for(str(wid))
    monkeypatch.setattr("anx_agent_bridge.bridge.time.monotonic", lambda: now["value"])
    bridge._failure_counts["wake-backoff"] = 1
    bridge._next_retry_at_monotonic["wake-backoff"] = 160.0
    client.notifications = [
        {
            "wakeup_id": "wake-backoff",
            "status": "unread",
            "target_actor_id": "actor-hermes",
            "thread_id": "thread-1",
            "request_event_id": "evt-req-b",
            "trigger_event_id": "evt-trig-b",
        },
        {
            "wakeup_id": "wake-good",
            "status": "unread",
            "target_actor_id": "actor-hermes",
            "thread_id": "thread-1",
            "request_event_id": "evt-req-g",
            "trigger_event_id": "evt-trig-g",
        },
    ]

    bridge._drain_notifications()

    assert len(bridge.adapter.dispatch_calls) == 1
    assert bridge.adapter.dispatch_calls[0]["packet"].wakeup_id == "wake-good"
    assert client.notification_reads == ["wake-good"]


def test_drain_notifications_continues_after_dispatch_failure():
    bridge, _state, client = build_bridge([])
    calls = {"n": 0}
    base_artifact = client.get_artifact_content("wake-1")

    def artifact_for(wakeup_id: str):
        return {**base_artifact, "wakeup_id": wakeup_id}

    client.get_artifact_content = lambda wid: artifact_for(str(wid))

    def counting_dispatch(*_a, **_k):
        calls["n"] += 1
        if calls["n"] == 1:
            raise RuntimeError("adapter down")
        from anx_agent_bridge.adapters.base import AdapterResult

        return AdapterResult(response_text="ok", native_session_id=None)

    bridge.adapter.dispatch = counting_dispatch
    client.notifications = [
        {
            "wakeup_id": "wake-bad",
            "status": "unread",
            "target_actor_id": "actor-hermes",
            "thread_id": "thread-1",
            "request_event_id": "evt-req-b",
            "trigger_event_id": "evt-trig-b",
        },
        {
            "wakeup_id": "wake-good",
            "status": "unread",
            "target_actor_id": "actor-hermes",
            "thread_id": "thread-1",
            "request_event_id": "evt-req-g",
            "trigger_event_id": "evt-trig-g",
        },
    ]

    bridge._drain_notifications()

    assert calls["n"] == 2
    assert len(client.failed_wakeups) == 1
    assert client.failed_wakeups[0]["wakeup_id"] == "wake-bad"
    assert client.completed_wakeups == [{"wakeup_id": "wake-good", "bridge_instance_id": "bridge-test"}]


def test_openclaw_subprocess_runtime_failure_marks_wake_failed(tmp_path: Path):
    bridge, state, client = build_bridge([])
    fake_openclaw = tmp_path / "openclaw"
    fake_openclaw.write_text(
        "#!/usr/bin/env python3\n"
        "import sys\n"
        "sys.stderr.write('gateway unavailable\\n')\n"
        "sys.exit(7)\n",
        encoding="utf-8",
    )
    fake_openclaw.chmod(0o755)
    fake_anx = tmp_path / "anx"
    fake_anx.write_text("#!/usr/bin/env python3\n", encoding="utf-8")
    fake_anx.chmod(0o755)
    bridge.adapter = SubprocessAdapter(
        command=[sys.executable, "-m", "anx_agent_bridge.adapters.openclaw"],
        handle="hermes",
        workspace_id="ws_main",
        dispatch_timeout_seconds=10,
        adapter_raw={
            "kind": "openclaw",
            "openclaw_bin": str(fake_openclaw),
            "anx_cli_bin": str(fake_anx),
        },
    )

    bridge._handle_notification(
        {
            "wakeup_id": "wake-1",
            "target_actor_id": "actor-hermes",
            "thread_id": "thread-1",
            "request_event_id": "evt-request",
            "trigger_event_id": "evt-trigger",
        }
    )

    assert state.handled_wakeup_ids() == set()
    assert client.created_events == []
    assert client.completed_wakeups == []
    assert len(client.failed_wakeups) == 1
    assert client.failed_wakeups[0]["wakeup_id"] == "wake-1"
    assert "adapter dispatch command exited 1" in client.failed_wakeups[0]["error"]
    assert "OpenClaw exited 7: gateway unavailable" in client.failed_wakeups[0]["error"]


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
    assert client.completed_wakeups == [{"wakeup_id": "wake-1", "bridge_instance_id": "bridge-test"}]
    assert client.failed_wakeups == []


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
    assert len(client.bridge_checkins) == 1
    reg_payload = client.registration_updates[0]["registration"]
    assert reg_payload["status"] == "active"
    assert reg_payload["bridge_instance_id"] == "bridge-test"
    assert reg_payload["bridge_signing_public_key_spki_b64"] != ""
    assert reg_payload["bridge_checked_in_at"] != ""
    assert reg_payload["bridge_expires_at"] != ""
    assert reg_payload["bridge_workspace_ids"] == ["ws_main"]
    assert reg_payload["bridge_proof_signature_b64"] != ""
    checkin_payload = client.bridge_checkins[0]
    assert checkin_payload["bridge_instance_id"] == "bridge-test"
    assert checkin_payload["workspace_id"] == "ws_main"
    assert checkin_payload["workspace_ids"] == ["ws_main"]
    assert checkin_payload["proof_signature_b64"] != ""


def test_bridge_checkin_advertises_same_core_workspace_ids():
    bridge, _state, client = build_bridge([], workspace_ids=["ws_main", "ws_aux"])

    bridge._publish_checkin()

    reg_payload = client.registration_updates[0]["registration"]
    checkin_payload = client.bridge_checkins[0]
    assert bridge.config.anx.base_url == "http://anx.test"
    assert reg_payload["bridge_workspace_ids"] == ["ws_main", "ws_aux"]
    assert [item["workspace_id"] for item in reg_payload["workspace_bindings"]] == ["ws_main", "ws_aux"]
    assert checkin_payload["workspace_id"] == "ws_main"
    assert checkin_payload["workspace_ids"] == ["ws_main", "ws_aux"]


def test_bridge_checkin_does_not_invoke_adapter_doctor():
    bridge, _state, client = build_bridge([])

    class NoDoctorAdapter:
        def doctor(self):
            raise RuntimeError("adapter doctor should not run during check-in")

        def dispatch(self, *_args, **_kwargs):
            raise NotImplementedError

    bridge.adapter = NoDoctorAdapter()
    bridge._publish_checkin()

    assert len(client.registration_updates) == 1
    assert client.registration_updates[0]["registration"]["status"] == "active"
