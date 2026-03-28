import pytest

from oar_agent_bridge import __version__
from oar_agent_bridge.cli import build_parser


def test_version_flag_prints_package_version(capsys):
    parser = build_parser()

    with pytest.raises(SystemExit) as excinfo:
        parser.parse_args(["--version"])

    assert excinfo.value.code == 0
    captured = capsys.readouterr()
    assert f"oar-agent-bridge {__version__}" in captured.out


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
