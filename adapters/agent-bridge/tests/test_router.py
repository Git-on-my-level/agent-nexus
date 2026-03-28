from pathlib import Path

from oar_agent_bridge.config import AdapterConfig, LoadedConfig, OARConfig, RouterConfig
from oar_agent_bridge.models import AgentBridgeCheckin, AgentRegistration, WorkspaceBinding
from oar_agent_bridge.router import WakeRouter


class StubClient:
    def __init__(self):
        self.recorded = []

    def create_event(self, **kwargs):
        self.recorded.append(kwargs)
        return {}


class StubState:
    last_event_id = None


def test_emit_exception_includes_required_subtype():
    config = LoadedConfig(
        oar=OARConfig(base_url="http://oar.test", workspace_id="ws_main", workspace_name="Main"),
        agent=None,
        router=RouterConfig(state_path=Path("/tmp/router-state.json")),
        adapter=AdapterConfig(raw={}),
        auth_state_path=Path("/tmp/router-auth.json"),
    )
    client = StubClient()
    router = WakeRouter(config, client, StubState())

    router._emit_exception("thread-1", "event-1", "hermes", "unknown_agent_handle", "Unknown tagged agent @hermes")

    payload = client.recorded[0]["event"]["payload"]
    assert payload["subtype"] == "unknown_agent_handle"
    assert payload["code"] == "unknown_agent_handle"


def test_route_mention_requires_bridge_checkin_before_wake():
    config = LoadedConfig(
        oar=OARConfig(base_url="http://oar.test", workspace_id="ws_main", workspace_name="Main"),
        agent=None,
        router=RouterConfig(state_path=Path("/tmp/router-state.json")),
        adapter=AdapterConfig(raw={}),
        auth_state_path=Path("/tmp/router-auth.json"),
    )
    client = StubClient()
    router = WakeRouter(config, client, StubState())
    router._principal_cache.principals_by_handle = {
        "hermes": {
            "principal_kind": "agent",
            "username": "hermes",
            "actor_id": "actor-hermes",
        }
    }
    router._load_principals = lambda force=False: None
    router._load_registration = lambda handle: AgentRegistration(
        handle=handle,
        actor_id="actor-hermes",
        status="pending",
        workspace_bindings=[WorkspaceBinding(workspace_id="ws_main")],
    )

    router._route_mention(
        handle="hermes",
        event={"id": "event-1", "thread_id": "thread-1", "actor_id": "actor-human"},
        text="@hermes help",
    )

    payload = client.recorded[0]["event"]["payload"]
    assert payload["subtype"] == "agent_bridge_not_ready"


def test_route_mention_rejects_invalid_bridge_proof():
    config = LoadedConfig(
        oar=OARConfig(base_url="http://oar.test", workspace_id="ws_main", workspace_name="Main"),
        agent=None,
        router=RouterConfig(state_path=Path("/tmp/router-state.json")),
        adapter=AdapterConfig(raw={}),
        auth_state_path=Path("/tmp/router-auth.json"),
    )
    client = StubClient()
    router = WakeRouter(config, client, StubState())
    router._principal_cache.principals_by_handle = {
        "hermes": {
            "principal_kind": "agent",
            "username": "hermes",
            "actor_id": "actor-hermes",
        }
    }
    router._load_principals = lambda force=False: None
    router._load_registration = lambda handle: AgentRegistration(
        handle=handle,
        actor_id="actor-hermes",
        status="active",
        bridge_signing_public_key_spki_b64="invalid",
        bridge_checkin_event_id="event-bridge-checkin-1",
        workspace_bindings=[WorkspaceBinding(workspace_id="ws_main")],
    )
    router._load_bridge_checkin = lambda _event_id: AgentBridgeCheckin(
        handle="hermes",
        actor_id="actor-hermes",
        workspace_id="ws_main",
        bridge_instance_id="bridge-hermes-1",
        checked_in_at="2099-03-01T10:00:00Z",
        expires_at="2099-03-01T10:05:00Z",
        proof_signature_b64="invalid",
    )

    router._route_mention(
        handle="hermes",
        event={"id": "event-1", "thread_id": "thread-1", "actor_id": "actor-human"},
        text="@hermes help",
    )

    payload = client.recorded[0]["event"]["payload"]
    assert payload["subtype"] == "agent_bridge_proof_invalid"
