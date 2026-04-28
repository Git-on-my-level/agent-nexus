from __future__ import annotations

import os
import tomllib
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .util import ensure_dir, parse_bool


@dataclass(slots=True)
class ANXConfig:
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
class WorkspaceConfig:
    id: str
    name: str
    base_url: str
    enabled: bool = True
    url: str | None = None


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
    anx: ANXConfig
    agent: AgentConfig | None
    adapter: AdapterConfig
    auth_state_path: Path
    config_path: Path = field(default_factory=lambda: Path("bridge.toml"))
    config_dir: Path = field(default_factory=lambda: Path("."))
    agent_home: Path = field(default_factory=lambda: Path("."))
    agent_manifest_path: Path = field(default_factory=lambda: Path("agent.toml"))
    wake_config_path: Path = field(default_factory=lambda: Path("wake.toml"))
    expected_agent_id: str = ""
    expected_actor_id: str = ""
    expected_key_id: str = ""
    expected_public_key_fingerprint: str = ""
    workspaces: list[WorkspaceConfig] = field(default_factory=list)

    @property
    def workspace_ids(self) -> list[str]:
        ids = [workspace.id for workspace in self.workspaces if workspace.enabled and workspace.id]
        if ids:
            return ids
        if self.agent is not None and self.agent.workspace_bindings:
            return [item for item in self.agent.workspace_bindings if item]
        return [self.anx.workspace_id] if self.anx.workspace_id else []


def _expand_path(base_dir: Path, value: str | None, default: str) -> Path:
    raw = value or default
    expanded = os.path.expandvars(os.path.expanduser(raw))
    path = Path(expanded)
    if not path.is_absolute():
        path = (base_dir / path).resolve()
    ensure_dir(path.parent if path.suffix else path)
    return path


def _read_toml(path: Path) -> dict[str, Any]:
    with path.open("rb") as handle:
        data = tomllib.load(handle)
    if not isinstance(data, dict):
        return {}
    return data


def _config_string(table: dict[str, Any], key: str, default: str = "") -> str:
    return str(table.get(key, default)).strip()


def _resolve_agent_home(config_dir: Path, raw: str) -> Path:
    value = raw.strip()
    if not value:
        raise ValueError("bridge config requires top-level agent_home")
    return _expand_path(config_dir, value, ".anx")


def _load_workspaces(path: Path, default_base_url: str) -> list[WorkspaceConfig]:
    data = _read_toml(path)
    items = data.get("workspaces") or []
    workspaces: list[WorkspaceConfig] = []
    if not isinstance(items, list):
        raise ValueError("wake config requires [[workspaces]] entries")
    for item in items:
        if not isinstance(item, dict):
            continue
        workspace_id = _config_string(item, "id")
        if not workspace_id:
            continue
        workspaces.append(
            WorkspaceConfig(
                id=workspace_id,
                name=_config_string(item, "name", workspace_id) or workspace_id,
                base_url=_config_string(item, "base_url", default_base_url).rstrip("/"),
                enabled=parse_bool(item.get("enabled", True), default=True),
                url=_config_string(item, "url") or None,
            )
        )
    if not any(workspace.enabled for workspace in workspaces):
        raise ValueError("wake config requires at least one enabled workspace")
    return workspaces


def load_config(path: str | os.PathLike[str]) -> LoadedConfig:
    config_path = Path(path).resolve()
    config_dir = config_path.parent
    data = _read_toml(config_path)

    agent_home = _resolve_agent_home(config_dir, _config_string(data, "agent_home"))
    agent_manifest_path = agent_home / "agent.toml"
    if not agent_manifest_path.exists():
        raise ValueError(f"agent home is missing agent.toml: {agent_manifest_path}")
    manifest = _read_toml(agent_manifest_path)
    identity_table = manifest.get("identity") or {}
    if not isinstance(identity_table, dict):
        identity_table = {}
    auth_table = manifest.get("auth") or {}
    if not isinstance(auth_table, dict):
        auth_table = {}
    bridge_table = data.get("bridge") or {}
    if not isinstance(bridge_table, dict):
        bridge_table = {}
    runtime_table = data.get("runtime") or {}
    if not isinstance(runtime_table, dict):
        runtime_table = {}

    base_url = _config_string(identity_table, "base_url").rstrip("/")
    handle = _config_string(identity_table, "handle")
    if not base_url or not handle:
        raise ValueError("agent.toml requires identity.base_url and identity.handle")

    wake_table = manifest.get("wake") or {}
    if not isinstance(wake_table, dict):
        wake_table = {}
    wake_config_path = _expand_path(
        agent_home,
        _config_string(data, "wake_config", _config_string(wake_table, "config_path", "wake.toml")),
        "wake.toml",
    )
    workspaces = _load_workspaces(wake_config_path, base_url)
    primary = next(workspace for workspace in workspaces if workspace.enabled)
    anx_cfg = ANXConfig(
        base_url=base_url,
        workspace_id=primary.id,
        workspace_name=primary.name,
        workspace_url=primary.url,
        verify_ssl=parse_bool(identity_table.get("verify_ssl", True), default=True),
    )
    auth_state_path = _expand_path(agent_home, _config_string(auth_table, "state_path", "profiles/default.json"), "profiles/default.json")

    state_dir = _expand_path(agent_home, _config_string(runtime_table, "state_dir", "run/default"), "run/default")
    agent_cfg = AgentConfig(
        handle=handle,
        driver_kind=_config_string(bridge_table, "driver_kind", "custom") or "custom",
        adapter_kind=_config_string(bridge_table, "adapter_kind", _config_string((data.get("adapter") or {}), "kind", "custom")) or "custom",
        state_dir=state_dir,
        workspace_bindings=[workspace.id for workspace in workspaces if workspace.enabled],
        resume_policy=_config_string(bridge_table, "resume_policy", "resume_or_create") or "resume_or_create",
        status=_config_string(bridge_table, "status", "pending") or "pending",
        checkin_interval_seconds=max(5, int(bridge_table.get("checkin_interval_seconds", 60))),
        checkin_ttl_seconds=max(30, int(bridge_table.get("checkin_ttl_seconds", 300))),
    )

    adapter = AdapterConfig(raw=data.get("adapter") or {})

    return LoadedConfig(
        anx=anx_cfg,
        agent=agent_cfg,
        adapter=adapter,
        auth_state_path=auth_state_path,
        config_path=config_path,
        config_dir=config_dir,
        agent_home=agent_home,
        agent_manifest_path=agent_manifest_path,
        wake_config_path=wake_config_path,
        expected_agent_id=_config_string(identity_table, "agent_id"),
        expected_actor_id=_config_string(identity_table, "actor_id"),
        expected_key_id=_config_string(identity_table, "key_id"),
        expected_public_key_fingerprint=_config_string(identity_table, "public_key_fingerprint"),
        workspaces=workspaces,
    )
