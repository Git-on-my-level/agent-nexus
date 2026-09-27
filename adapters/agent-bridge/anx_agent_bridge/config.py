"""One runtime configuration per enrolled host. No credentials live in this file."""
from __future__ import annotations

import os
import re
import tomllib
from dataclasses import dataclass
from pathlib import Path

NAME = re.compile(r"^[a-z][a-z0-9-]{0,31}$")


@dataclass(frozen=True)
class Runtime:
    name: str
    adapter: str
    command: tuple[str, ...]
    cwd: Path
    env: dict[str, str]


@dataclass(frozen=True)
class Config:
    path: Path
    base_url: str
    host_id: str
    host_slug: str
    anx: str
    agentctl: str
    state_dir: Path
    poll_seconds: float
    checkin_seconds: float
    runtimes: dict[str, Runtime]


def load_config(path: str | Path) -> Config:
    path = Path(path).expanduser().resolve()
    with path.open("rb") as stream:
        data = tomllib.load(stream)
    if any(key in data for key in ("agent_home", "wake_config", "adapter", "auth")):
        raise ValueError("obsolete per-agent bridge configuration")
    host = data.get("host", {})
    if not isinstance(host, dict):
        raise ValueError("[host] is required")
    base_url = str(host.get("base_url", "")).rstrip("/")
    host_id = str(host.get("id", "")).strip()
    host_slug = str(host.get("slug", "")).strip()
    if not base_url.startswith(("http://", "https://")) or not host_id or not host_slug:
        raise ValueError("[host] requires base_url, id, and slug from `anx host enroll`")
    if not re.fullmatch(r"[a-z0-9-]+", host_slug):
        raise ValueError("host.slug must be a lowercase host slug")
    runtime_data = data.get("agents", {})
    if not isinstance(runtime_data, dict) or not runtime_data:
        raise ValueError("at least one [agents.<name>] runtime is required")
    runtimes = {}
    for name, entry in runtime_data.items():
        if not NAME.fullmatch(name) or not isinstance(entry, dict):
            raise ValueError(f"invalid runtime name: {name!r}")
        if not entry.get("enabled", True):
            continue
        command = entry.get("command")
        if not isinstance(command, list) or not command or any(not isinstance(x, str) or not x for x in command):
            raise ValueError(f"agents.{name}.command must be a nonempty argv array")
        default_adapter = name if name in ("claude", "codex", "cursor", "omp", "generic") else "generic"
        adapter = str(entry.get("adapter", default_adapter)).strip()
        if not NAME.fullmatch(adapter):
            raise ValueError(f"agents.{name}.adapter is invalid")
        cwd = Path(os.path.expandvars(str(entry.get("cwd", path.parent)))).expanduser()
        if not cwd.is_absolute():
            cwd = path.parent / cwd
        env = entry.get("env", {})
        if not isinstance(env, dict) or any(not isinstance(k, str) or not isinstance(v, str) for k, v in env.items()):
            raise ValueError(f"agents.{name}.env must be a string table")
        runtimes[name] = Runtime(name, adapter, tuple(command), cwd.resolve(), env)
    if not runtimes:
        raise ValueError("at least one enabled runtime is required")
    state = Path(os.path.expandvars(str(host.get("state_dir", "~/.local/state/anx/bridge")))).expanduser()
    if not state.is_absolute():
        state = path.parent / state
    poll = float(host.get("poll_seconds", 3))
    checkin = float(host.get("checkin_seconds", 60))
    if poll <= 0 or not 5 <= checkin <= 240:
        raise ValueError("poll_seconds must be positive; checkin_seconds must be 5..240")
    return Config(path, base_url, host_id, host_slug, str(host.get("anx", "anx")),
                  str(host.get("agentctl", "agentctl")), state.resolve(), poll, checkin, runtimes)
