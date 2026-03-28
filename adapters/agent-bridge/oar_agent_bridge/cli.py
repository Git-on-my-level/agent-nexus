from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from . import __version__
from .auth import AuthManager
from .bridge import AgentBridge
from .config import LoadedConfig, load_config
from .oar_client import OARClient
from .registry import apply_registration, registration_status
from .router import WakeRouter
from .adapters import HermesACPAdapter, ZeroClawGatewayAdapter
from .state_store import JSONStateStore
from .util import configure_logging


def build_client(config: LoadedConfig, auth: AuthManager | None = None) -> OARClient:
    return OARClient(config.oar.base_url, verify_ssl=config.oar.verify_ssl, auth_manager=auth)


def build_adapter(config: LoadedConfig):
    kind = config.adapter.get_str("kind", config.agent.adapter_kind if config.agent else "")
    if kind == "hermes_acp":
        command = config.adapter.get_list("command") or ["hermes", "acp"]
        cwd_default = config.adapter.require_str("cwd_default")
        workspace_map = config.adapter.get_table("workspace_map")
        env = config.adapter.raw.get("env") if isinstance(config.adapter.raw.get("env"), dict) else None
        env_str = {str(k): str(v) for k, v in (env or {}).items()}
        return HermesACPAdapter(
            command=command,
            cwd_default=cwd_default,
            workspace_map=workspace_map,
            env=env_str or None,
            auto_select_permission=config.adapter.get_bool("auto_select_permission", True),
            permission_option_id=config.adapter.get_str("permission_option_id", "") or None,
        )
    if kind == "zeroclaw_gateway":
        return ZeroClawGatewayAdapter(
            base_url=config.adapter.require_str("base_url"),
            bearer_token=config.adapter.require_str("bearer_token"),
            webhook_secret=config.adapter.get_str("webhook_secret", "") or None,
            request_timeout_seconds=config.adapter.get_int("request_timeout_seconds", 600),
            session_header_name=config.adapter.get_str("session_header_name", "X-Session-Id") or "X-Session-Id",
        )
    raise ValueError(f"Unsupported adapter kind: {kind}")


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
            result["registration_document_id"] = reg_result.document_id
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
        print(json.dumps(result.__dict__, indent=2))
        return 0
    finally:
        client.close()


def cmd_registration_status(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    try:
        result = registration_status(config, auth, client)
        print(json.dumps(result.__dict__, indent=2))
        return 0
    finally:
        client.close()


def cmd_router_run(args: argparse.Namespace) -> int:
    config = load_config(args.config)
    if config.router is None:
        raise ValueError("router run requires a [router] section")
    auth = AuthManager(config.auth_state_path)
    client = build_client(config, auth)
    state = JSONStateStore(config.router.state_path)
    router = WakeRouter(config, client, state)
    router.run_forever()
    return 0


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
        "workspace_id": config.oar.workspace_id,
        "bridge_instance_id": state.bridge_instance_id,
        "adapter": bridge.doctor(),
    }
    print(json.dumps(result, indent=2))
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="oar-agent-bridge")
    parser.add_argument("--verbose", action="store_true")
    parser.add_argument("--version", action="version", version=f"oar-agent-bridge {__version__}")
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

    router_parser = subparsers.add_parser("router")
    router_sub = router_parser.add_subparsers(dest="router_command", required=True)
    router_run = router_sub.add_parser("run")
    router_run.add_argument("--config", required=True)
    router_run.set_defaults(func=cmd_router_run)

    bridge_parser = subparsers.add_parser("bridge")
    bridge_sub = bridge_parser.add_subparsers(dest="bridge_command", required=True)
    bridge_run = bridge_sub.add_parser("run")
    bridge_run.add_argument("--config", required=True)
    bridge_run.set_defaults(func=cmd_bridge_run)
    bridge_doctor = bridge_sub.add_parser("doctor")
    bridge_doctor.add_argument("--config", required=True)
    bridge_doctor.set_defaults(func=cmd_bridge_doctor)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    configure_logging(verbose=bool(args.verbose))
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())
