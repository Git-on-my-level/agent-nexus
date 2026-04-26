from __future__ import annotations

import argparse
import json
from dataclasses import asdict

from . import __version__
from .auth import AuthManager
from .bridge import AgentBridge
from .config import LoadedConfig, load_config
from .anx_client import ANXClient
from .registry import apply_registration, registration_status
from .adapters import (
    DeterministicAckAdapter,
    RESPONSE_SCHEMA_VERSION,
    SubprocessAdapter,
    build_dispatch_request,
    build_doctor_request,
    load_plugin_adapter,
    sample_wake_packet_for_contract,
)
from .state_store import JSONStateStore
from .util import configure_logging


def build_client(config: LoadedConfig, auth: AuthManager | None = None) -> ANXClient:
    return ANXClient(config.anx.base_url, verify_ssl=config.anx.verify_ssl, auth_manager=auth)


def build_adapter(config: LoadedConfig):
    if config.agent is None:
        raise ValueError("bridge adapter requires [agent] in config")
    kind = config.adapter.get_str("kind", config.agent.adapter_kind).strip()
    if not kind:
        raise ValueError("adapter kind is empty; set [adapter].kind or [agent].adapter_kind")
    if kind == "subprocess":
        command = config.adapter.get_list("command")
        if not command:
            raise ValueError("subprocess adapter requires [adapter].command as a non-empty string array")
        cwd = config.adapter.get_str("cwd", "") or None
        env_raw = config.adapter.raw.get("env")
        env_str: dict[str, str] | None = None
        if isinstance(env_raw, dict):
            env_str = {str(k): str(v) for k, v in env_raw.items()}
        doctor_raw = config.adapter.raw.get("doctor_command")
        doctor_command: list[str] | None = None
        if isinstance(doctor_raw, list) and len(doctor_raw) > 0:
            doctor_command = [str(x) for x in doctor_raw]
        return SubprocessAdapter(
            command=command,
            handle=config.agent.handle,
            workspace_id=config.anx.workspace_id,
            cwd=cwd,
            env=env_str,
            dispatch_timeout_seconds=config.adapter.get_int("timeout_seconds", 600),
            doctor_timeout_seconds=config.adapter.get_int("doctor_timeout_seconds", 60),
            doctor_command=doctor_command,
            adapter_raw=dict(config.adapter.raw),
        )
    if kind == "python_plugin":
        mod = config.adapter.require_str("plugin_module")
        fac = config.adapter.require_str("plugin_factory")
        return load_plugin_adapter(mod, fac, dict(config.adapter.raw))
    if kind == "deterministic_ack":
        return DeterministicAckAdapter()
    raise ValueError(
        f"Unsupported adapter kind: {kind!r}; use subprocess, python_plugin, or deterministic_ack (tests only)"
    )


def cmd_auth_register(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    auth = AuthManager(config.auth_state_path)
    client = build_client(config)
    try:
        username = args.username or (config.agent.handle if config.agent else None)
        if not username:
            raise ValueError("--username is required when config has no [agent] section")
        state = auth.register(client, username=username, bootstrap_token=args.bootstrap_token, invite_token=args.invite_token)
        result = {
            "username": state.username,
            "agent_id": state.agent_id,
            "actor_id": state.actor_id,
            "key_id": state.key_id,
            "auth_state_path": str(config.auth_state_path),
        }
        if args.apply_registration and config.agent is not None:
            reg_result = apply_registration(config, auth, build_client(config, auth))
            result["registration_agent_id"] = reg_result.agent_id
        print(json.dumps(result, indent=2))
        return 0
    finally:
        client.close()


def cmd_auth_whoami(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    try:
        payload = auth.whoami(client)
        print(json.dumps(payload, indent=2))
        return 0
    finally:
        client.close()


def cmd_registration_apply(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    try:
        result = apply_registration(config, auth, client)
        print(json.dumps(asdict(result), indent=2))
        return 0
    finally:
        client.close()


def cmd_registration_status(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    try:
        result = registration_status(config, auth, client)
        print(json.dumps(asdict(result), indent=2))
        return 0
    finally:
        client.close()


def cmd_bridge_run(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    if config.agent is None:
        raise ValueError("bridge run requires an [agent] section")
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    state_path = config.agent.state_dir / "bridge-state.json"
    state = JSONStateStore(state_path, ensure_bridge_identity=True)
    adapter = build_adapter(config)
    bridge = AgentBridge(config, auth, client, state, adapter)
    bridge.run_forever()
    return 0


def cmd_bridge_doctor(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    if config.agent is None:
        raise ValueError("bridge doctor requires an [agent] section")
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    state_path = config.agent.state_dir / "bridge-state.json"
    state = JSONStateStore(state_path, ensure_bridge_identity=True)
    adapter = build_adapter(config)
    bridge = AgentBridge(config, auth, client, state, adapter)
    result = {
        "handle": config.agent.handle,
        "workspace_id": config.anx.workspace_id,
        "bridge_instance_id": state.bridge_instance_id,
        "adapter": bridge.doctor(),
    }
    print(json.dumps(result, indent=2))
    return 0


def cmd_adapter_contract(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    if config.agent is None:
        raise ValueError("adapter contract requires an [agent] section")
    packet = sample_wake_packet_for_contract(
        handle=config.agent.handle,
        workspace_id=config.anx.workspace_id,
        workspace_name=config.anx.workspace_name,
        anx_base_url=config.anx.base_url,
    )
    adapter_table = dict(config.adapter.raw)
    dispatch_example = build_dispatch_request(
        wake_packet=packet,
        prompt_text="Example prompt passed to your adapter.",
        session_key=packet.session_key,
        existing_native_session_id=None,
        adapter_settings=adapter_table,
    )
    doctor_example = build_doctor_request(
        handle=config.agent.handle,
        workspace_id=config.anx.workspace_id,
        adapter_settings=adapter_table,
    )
    out = {
        "subprocess_environment": {"ANX_BRIDGE_MODE": "dispatch | doctor"},
        "stdin_json_dispatch": dispatch_example,
        "stdin_json_doctor": doctor_example,
        "stdout_json_dispatch": {
            "schema_version": RESPONSE_SCHEMA_VERSION,
            "response_text": "Reply text posted back to ANX (may be empty string).",
            "native_session_id": "optional-native-session-id",
            "metadata": {},
        },
        "stdout_json_doctor": {
            "schema_version": RESPONSE_SCHEMA_VERSION,
            "ok": True,
            "message": "optional human-readable status",
            "details": {},
        },
    }
    print(json.dumps(out, indent=2))
    return 0


def cmd_notifications_list(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    try:
        statuses = [str(item).strip() for item in (args.status or []) if str(item).strip()]
        payload = client.list_agent_notifications(statuses=statuses or None, order=args.order)
        print(json.dumps({"items": payload}, indent=2))
        return 0
    finally:
        client.close()


def cmd_notifications_read(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    try:
        payload = client.mark_agent_notification_read(args.wakeup_id)
        print(json.dumps(payload, indent=2))
        return 0
    finally:
        client.close()


def cmd_notifications_dismiss(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    try:
        payload = client.dismiss_agent_notification(args.wakeup_id)
        print(json.dumps(payload, indent=2))
        return 0
    finally:
        client.close()


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="anx-agent-bridge")
    parser.add_argument("--verbose", action="store_true")
    parser.add_argument("--version", action="version", version=f"anx-agent-bridge {__version__}")
    subparsers = parser.add_subparsers(dest="command", required=True)

    auth_parser = subparsers.add_parser("auth")
    auth_sub = auth_parser.add_subparsers(dest="auth_command", required=True)

    register_parser = auth_sub.add_parser("register")
    register_parser.add_argument("--config", required=True)
    register_parser.add_argument("--username")
    register_parser.add_argument("--bootstrap-token")
    register_parser.add_argument("--invite-token")
    register_parser.add_argument("--apply-registration", action="store_true")
    register_parser.set_defaults(func=cmd_auth_register)

    whoami_parser = auth_sub.add_parser("whoami")
    whoami_parser.add_argument("--config", required=True)
    whoami_parser.set_defaults(func=cmd_auth_whoami)

    reg_parser = subparsers.add_parser("registration")
    reg_sub = reg_parser.add_subparsers(dest="registration_command", required=True)
    reg_apply_parser = reg_sub.add_parser("apply")
    reg_apply_parser.add_argument("--config", required=True)
    reg_apply_parser.set_defaults(func=cmd_registration_apply)
    reg_status_parser = reg_sub.add_parser("status")
    reg_status_parser.add_argument("--config", required=True)
    reg_status_parser.set_defaults(func=cmd_registration_status)

    bridge_parser = subparsers.add_parser("bridge")
    bridge_sub = bridge_parser.add_subparsers(dest="bridge_command", required=True)
    bridge_run = bridge_sub.add_parser("run")
    bridge_run.add_argument("--config", required=True)
    bridge_run.set_defaults(func=cmd_bridge_run)
    bridge_doctor = bridge_sub.add_parser("doctor")
    bridge_doctor.add_argument("--config", required=True)
    bridge_doctor.set_defaults(func=cmd_bridge_doctor)

    adapter_parser = subparsers.add_parser("adapter")
    adapter_sub = adapter_parser.add_subparsers(dest="adapter_command", required=True)
    contract_parser = adapter_sub.add_parser("contract")
    contract_parser.add_argument("--config", required=True)
    contract_parser.set_defaults(func=cmd_adapter_contract)

    notifications_parser = subparsers.add_parser("notifications")
    notifications_sub = notifications_parser.add_subparsers(dest="notifications_command", required=True)
    notifications_list = notifications_sub.add_parser("list")
    notifications_list.add_argument("--config", required=True)
    notifications_list.add_argument("--status", action="append", choices=["unread", "read", "dismissed"])
    notifications_list.add_argument("--order", choices=["asc", "desc"], default="desc")
    notifications_list.set_defaults(func=cmd_notifications_list)

    notifications_read = notifications_sub.add_parser("read")
    notifications_read.add_argument("--config", required=True)
    notifications_read.add_argument("--wakeup-id", required=True)
    notifications_read.set_defaults(func=cmd_notifications_read)

    notifications_dismiss = notifications_sub.add_parser("dismiss")
    notifications_dismiss.add_argument("--config", required=True)
    notifications_dismiss.add_argument("--wakeup-id", required=True)
    notifications_dismiss.set_defaults(func=cmd_notifications_dismiss)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    configure_logging(verbose=bool(args.verbose))
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())
