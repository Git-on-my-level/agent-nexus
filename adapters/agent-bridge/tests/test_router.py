from pathlib import Path

from oar_agent_bridge.config import AdapterConfig, LoadedConfig, OARConfig, RouterConfig
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
