import argparse

import pytest

from anx_agent_bridge import __version__
from anx_agent_bridge import cli as cli_module
from anx_agent_bridge.cli import build_parser
from anx_agent_bridge.registry import RegistrationStatusResult


def test_version_flag_prints_package_version(capsys):
    parser = build_parser()

    with pytest.raises(SystemExit) as excinfo:
        parser.parse_args(["--version"])

    assert excinfo.value.code == 0
    captured = capsys.readouterr()
    assert f"anx-agent-bridge {__version__}" in captured.out


def test_registration_status_subcommand_is_available():
    parser = build_parser()

    args = parser.parse_args(["registration", "status", "--config", "agent.toml"])

    assert args.command == "registration"
    assert args.registration_command == "status"
    assert args.config == "agent.toml"


def test_bridge_doctor_subcommand_is_available():
    parser = build_parser()

    args = parser.parse_args(["bridge", "doctor", "--config", "agent.toml"])

    assert args.command == "bridge"
    assert args.bridge_command == "doctor"
    assert args.config == "agent.toml"


def test_notifications_subcommands_are_available():
    parser = build_parser()

    listed = parser.parse_args(["notifications", "list", "--config", "agent.toml", "--status", "unread"])
    read = parser.parse_args(["notifications", "read", "--config", "agent.toml", "--wakeup-id", "wake_123"])
    dismiss = parser.parse_args(["notifications", "dismiss", "--config", "agent.toml", "--wakeup-id", "wake_123"])

    assert listed.command == "notifications"
    assert listed.notifications_command == "list"
    assert listed.status == ["unread"]
    assert read.notifications_command == "read"
    assert read.wakeup_id == "wake_123"
    assert dismiss.notifications_command == "dismiss"
    assert dismiss.wakeup_id == "wake_123"


def test_router_subcommand_is_not_available():
    parser = build_parser()

    with pytest.raises(SystemExit) as excinfo:
        parser.parse_args(["router", "run", "--config", "router.toml"])

    assert excinfo.value.code == 2


def test_cmd_registration_status_serializes_slots_dataclass(monkeypatch, capsys):
    closed = {"value": False}
    config = argparse.Namespace(auth_state_path="state.json")

    class DummyClient:
        def close(self):
            closed["value"] = True

    monkeypatch.setattr(cli_module, "load_config", lambda _path: config)
    monkeypatch.setattr(cli_module, "AuthManager", lambda _path: object())
    monkeypatch.setattr(cli_module, "build_client", lambda _config, _auth: DummyClient())
    monkeypatch.setattr(
        cli_module,
        "registration_status",
        lambda _config, _auth, _client: RegistrationStatusResult(
            agent_id="agent-hermes",
            handle="hermes",
            actor_id="actor-1",
            registration_status="active",
            workspace_id="ws_main",
            workspace_bound=True,
            bridge_checkin_event_id="event-1",
            bridge_checked_in_at="2026-03-29T00:00:00Z",
            bridge_expires_at="2026-03-29T00:05:00Z",
            wakeable=True,
            blockers=[],
        ),
    )

    result = cli_module.cmd_registration_status(argparse.Namespace(config="agent.toml"))

    assert result == 0
    assert closed["value"] is True
    captured = capsys.readouterr()
    assert '"agent_id": "agent-hermes"' in captured.out
    assert '"wakeable": true' in captured.out


def test_cmd_notifications_list_serializes_payload(monkeypatch, capsys):
    closed = {"value": False}
    config = argparse.Namespace(auth_state_path="state.json")

    class DummyClient:
        def list_agent_notifications(self, *, statuses=None, order="desc"):
            assert statuses == ["unread"]
            assert order == "asc"
            return [{"wakeup_id": "wake_123", "status": "unread"}]

        def close(self):
            closed["value"] = True

    monkeypatch.setattr(cli_module, "load_config", lambda _path: config)
    monkeypatch.setattr(cli_module, "AuthManager", lambda _path: object())
    monkeypatch.setattr(cli_module, "build_client", lambda _config, _auth: DummyClient())

    result = cli_module.cmd_notifications_list(
        argparse.Namespace(config="agent.toml", status=["unread"], order="asc")
    )

    assert result == 0
    assert closed["value"] is True
    assert '"wakeup_id": "wake_123"' in capsys.readouterr().out
