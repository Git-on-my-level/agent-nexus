from pathlib import Path

from oar_agent_bridge.config import load_config


def test_load_config_parses_false_like_verify_ssl(tmp_path: Path):
    config_path = tmp_path / "bridge.toml"
    config_path.write_text(
        """
[oar]
base_url = "https://oar.example"
workspace_id = "ws_main"
workspace_name = "Main"
verify_ssl = "false"
""".strip()
        + "\n",
        encoding="utf-8",
    )

    loaded = load_config(config_path)

    assert loaded.oar.verify_ssl is False


def test_load_config_defaults_agent_checkin_lifecycle(tmp_path: Path):
    config_path = tmp_path / "bridge.toml"
    config_path.write_text(
        """
[oar]
base_url = "https://oar.example"
workspace_id = "ws_main"
workspace_name = "Main"

[agent]
handle = "hermes"
driver_kind = "acp"
adapter_kind = "hermes_acp"
state_dir = ".state/hermes"
workspace_bindings = ["ws_main"]
""".strip()
        + "\n",
        encoding="utf-8",
    )

    loaded = load_config(config_path)

    assert loaded.agent is not None
    assert loaded.agent.status == "pending"
    assert loaded.agent.checkin_interval_seconds == 60
    assert loaded.agent.checkin_ttl_seconds == 300
