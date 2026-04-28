from pathlib import Path

from anx_agent_bridge.config import load_config


def write_agent_home(tmp_path: Path, *, verify_ssl: str = "true") -> None:
    agent_home = tmp_path / ".anx"
    agent_home.mkdir()
    (agent_home / "agent.toml").write_text(
        f"""
schema_version = 1

[identity]
base_url = "https://anx.example"
handle = "myagent"
verify_ssl = {verify_ssl!r}

[auth]
state_path = "profiles/default.json"
""".strip()
        + "\n",
        encoding="utf-8",
    )
    (agent_home / "wake.toml").write_text(
        """
schema_version = 1

[[workspaces]]
id = "ws_main"
name = "Main"
enabled = true
""".strip()
        + "\n",
        encoding="utf-8",
    )


def test_load_config_parses_false_like_verify_ssl(tmp_path: Path):
    write_agent_home(tmp_path, verify_ssl="false")
    config_path = tmp_path / "bridge.toml"
    config_path.write_text(
        """
agent_home = ".anx"
""".strip()
        + "\n",
        encoding="utf-8",
    )

    loaded = load_config(config_path)

    assert loaded.anx.verify_ssl is False


def test_load_config_defaults_agent_checkin_lifecycle(tmp_path: Path):
    write_agent_home(tmp_path)
    config_path = tmp_path / "bridge.toml"
    config_path.write_text(
        """
agent_home = ".anx"

[bridge]
driver_kind = "custom"
adapter_kind = "subprocess"
""".strip()
        + "\n",
        encoding="utf-8",
    )

    loaded = load_config(config_path)

    assert loaded.agent is not None
    assert loaded.agent.status == "pending"
    assert loaded.agent.checkin_interval_seconds == 60
    assert loaded.agent.checkin_ttl_seconds == 300


def test_load_config_ignores_legacy_router_section(tmp_path: Path):
    write_agent_home(tmp_path)
    config_path = tmp_path / "bridge.toml"
    config_path.write_text(
        """
agent_home = ".anx"

[router]
state_path = ".state/router-state.json"
""".strip()
        + "\n",
        encoding="utf-8",
    )

    loaded = load_config(config_path)

    assert loaded.agent is not None
    assert loaded.auth_state_path.name == "default.json"
