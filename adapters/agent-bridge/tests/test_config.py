from pathlib import Path

import pytest

from anx_agent_bridge.config import load_config


def write_agent_home(tmp_path: Path, *, verify_ssl: str = "true", wake_config: str | None = None) -> None:
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
        (
            wake_config
            or """
schema_version = 1

[[workspaces]]
id = "ws_main"
name = "Main"
enabled = true
"""
        ).strip()
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


def test_load_config_does_not_create_auth_or_state_directories(tmp_path: Path):
    write_agent_home(tmp_path)
    config_path = tmp_path / "bridge.toml"
    config_path.write_text(
        """
agent_home = ".anx"

[runtime]
state_dir = "run/default"
""".strip()
        + "\n",
        encoding="utf-8",
    )

    loaded = load_config(config_path)

    assert loaded.auth_state_path == tmp_path / ".anx" / "profiles" / "default.json"
    assert loaded.agent is not None
    assert loaded.agent.state_dir == tmp_path / ".anx" / "run" / "default"
    assert not (tmp_path / ".anx" / "profiles").exists()
    assert not (tmp_path / ".anx" / "run").exists()


def test_load_config_uses_primary_workspace_base_url(tmp_path: Path):
    write_agent_home(
        tmp_path,
        wake_config="""
schema_version = 1

[[workspaces]]
id = "ws_main"
name = "Main"
base_url = "https://core-a.example/"
enabled = true
""",
    )
    config_path = tmp_path / "bridge.toml"
    config_path.write_text('agent_home = ".anx"\n', encoding="utf-8")

    loaded = load_config(config_path)

    assert loaded.anx.base_url == "https://core-a.example"
    assert loaded.workspace_ids == ["ws_main"]


def test_load_config_allows_multiple_enabled_workspaces_on_same_base_url(tmp_path: Path):
    write_agent_home(
        tmp_path,
        wake_config="""
schema_version = 1

[[workspaces]]
id = "ws_main"
name = "Main"
base_url = "https://core-a.example/"
enabled = true

[[workspaces]]
id = "ws_aux"
name = "Auxiliary"
base_url = "https://core-a.example"
enabled = true
""",
    )
    config_path = tmp_path / "bridge.toml"
    config_path.write_text('agent_home = ".anx"\n', encoding="utf-8")

    loaded = load_config(config_path)

    assert loaded.anx.base_url == "https://core-a.example"
    assert loaded.workspace_ids == ["ws_main", "ws_aux"]


def test_load_config_rejects_enabled_workspaces_on_different_base_urls(tmp_path: Path):
    write_agent_home(
        tmp_path,
        wake_config="""
schema_version = 1

[[workspaces]]
id = "ws_main"
name = "Main"
base_url = "https://core-a.example"
enabled = true

[[workspaces]]
id = "ws_other"
name = "Other"
base_url = "https://core-b.example"
enabled = true
""",
    )
    config_path = tmp_path / "bridge.toml"
    config_path.write_text('agent_home = ".anx"\n', encoding="utf-8")

    with pytest.raises(ValueError, match="multiple Agent Nexus base_url values"):
        load_config(config_path)


def test_load_config_ignores_disabled_workspace_base_url_when_validating_core(tmp_path: Path):
    write_agent_home(
        tmp_path,
        wake_config="""
schema_version = 1

[[workspaces]]
id = "ws_main"
name = "Main"
base_url = "https://core-a.example"
enabled = true

[[workspaces]]
id = "ws_disabled"
name = "Disabled"
base_url = "https://core-b.example"
enabled = false
""",
    )
    config_path = tmp_path / "bridge.toml"
    config_path.write_text('agent_home = ".anx"\n', encoding="utf-8")

    loaded = load_config(config_path)

    assert loaded.anx.base_url == "https://core-a.example"
    assert loaded.workspace_ids == ["ws_main"]
