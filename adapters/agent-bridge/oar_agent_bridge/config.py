from __future__ import annotations

import os
import tomllib
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .util import ensure_dir, parse_bool


@dataclass(slots=True)
class OARConfig:
    base_url: str
    workspace_id: str
    workspace_name: str
    workspace_url: str | None = None
    verify_ssl: bool = True


@dataclass(slots=True)
class AgentConfig:
    handle: str
    driver_kind: str
    adapter_kind: str
    state_dir: Path
    workspace_bindings: list[str] = field(default_factory=list)
    resume_policy: str = "resume_or_create"
    status: str = "pending"
    checkin_interval_seconds: int = 60
    checkin_ttl_seconds: int = 300


@dataclass(slots=True)
class RouterConfig:
    state_path: Path
    principal_cache_ttl_seconds: int = 60
    reconnect_delay_seconds: int = 3


@dataclass(slots=True)
class AdapterConfig:
    raw: dict[str, Any]

    def require_str(self, key: str) -> str:
        value = self.raw.get(key)
        if not isinstance(value, str) or not value.strip():
            raise ValueError(f"adapter.{key} is required")
        return value.strip()

    def get_str(self, key: str, default: str = "") -> str:
        value = self.raw.get(key, default)
        return str(value).strip()

    def get_bool(self, key: str, default: bool = False) -> bool:
        return parse_bool(self.raw.get(key, default), default=default)

    def get_int(self, key: str, default: int = 0) -> int:
        value = self.raw.get(key, default)
        return int(value)

    def get_list(self, key: str) -> list[str]:
        value = self.raw.get(key, [])
        if isinstance(value, list):
            return [str(item) for item in value]
        return []

    def get_table(self, key: str) -> dict[str, str]:
        value = self.raw.get(key, {})
        if not isinstance(value, dict):
            return {}
        return {str(k): str(v) for k, v in value.items()}


@dataclass(slots=True)
class LoadedConfig:
    oar: OARConfig
    agent: AgentConfig | None
    router: RouterConfig | None
    adapter: AdapterConfig
    auth_state_path: Path


def _expand_path(base_dir: Path, value: str | None, default: str) -> Path:
    raw = value or default
    expanded = os.path.expandvars(os.path.expanduser(raw))
    path = Path(expanded)
    if not path.is_absolute():
        path = (base_dir / path).resolve()
    ensure_dir(path.parent if path.suffix else path)
    return path


def load_config(path: str | os.PathLike[str]) -> LoadedConfig:
    config_path = Path(path).resolve()
    base_dir = config_path.parent
    with config_path.open("rb") as handle:
        data = tomllib.load(handle)

    oar_table = data.get("oar") or {}
    oar = OARConfig(
        base_url=str(oar_table.get("base_url", "")).rstrip("/"),
        workspace_id=str(oar_table.get("workspace_id", "")).strip(),
        workspace_name=str(oar_table.get("workspace_name", "")).strip(),
        workspace_url=str(oar_table.get("workspace_url", "")).strip() or None,
        verify_ssl=parse_bool(oar_table.get("verify_ssl", True), default=True),
    )
    if not oar.base_url or not oar.workspace_id or not oar.workspace_name:
        raise ValueError("config requires oar.base_url, oar.workspace_id, and oar.workspace_name")

    auth_state_path = _expand_path(base_dir, (data.get("auth") or {}).get("state_path"), ".state/auth.json")

    agent_cfg = None
    agent_table = data.get("agent") or None
    if agent_table:
        state_dir = _expand_path(base_dir, agent_table.get("state_dir"), ".state")
        agent_cfg = AgentConfig(
            handle=str(agent_table.get("handle", "")).strip(),
            driver_kind=str(agent_table.get("driver_kind", "custom")).strip() or "custom",
            adapter_kind=str(agent_table.get("adapter_kind", agent_table.get("driver_kind", "custom"))).strip() or "custom",
            state_dir=state_dir,
            workspace_bindings=[str(v).strip() for v in (agent_table.get("workspace_bindings") or []) if str(v).strip()],
            resume_policy=str(agent_table.get("resume_policy", "resume_or_create")).strip() or "resume_or_create",
            status=str(agent_table.get("status", "pending")).strip() or "pending",
            checkin_interval_seconds=max(5, int(agent_table.get("checkin_interval_seconds", 60))),
            checkin_ttl_seconds=max(30, int(agent_table.get("checkin_ttl_seconds", 300))),
        )
        if not agent_cfg.handle:
            raise ValueError("agent.handle is required when [agent] is present")

    router_cfg = None
    router_table = data.get("router") or None
    if router_table:
        state_path = _expand_path(base_dir, router_table.get("state_path"), ".state/router.json")
        router_cfg = RouterConfig(
            state_path=state_path,
            principal_cache_ttl_seconds=int(router_table.get("principal_cache_ttl_seconds", 60)),
            reconnect_delay_seconds=int(router_table.get("reconnect_delay_seconds", 3)),
        )

    adapter = AdapterConfig(raw=data.get("adapter") or {})

    return LoadedConfig(oar=oar, agent=agent_cfg, router=router_cfg, adapter=adapter, auth_state_path=auth_state_path)
