"""One runtime configuration per enrolled host. No credentials live in this file."""
from __future__ import annotations

import math
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
    config_dir: Path
    base_url: str
    host_id: str
    host_slug: str
    anx: str
    agentctl: str
    poll_seconds: float
    checkin_seconds: float
    runtimes: dict[str, Runtime]


def load_config(path: str | Path) -> Config:
    path = Path(path).expanduser().resolve()
    with path.open("rb") as stream:
        data = tomllib.load(stream)
    if any(key in data for key in ("agent_home", "wake_config", "adapter", "auth")):
        raise ValueError("obsolete per-agent bridge configuration")
    unexpected = set(data) - {"host", "agents"}
    if unexpected:
        raise ValueError(f"unknown bridge config sections: {sorted(unexpected)}")
    host = data.get("host", {})
    if not isinstance(host, dict):
        raise ValueError("[host] is required")
    unexpected = set(host) - {"base_url", "id", "slug", "anx", "agentctl", "poll_seconds", "checkin_seconds", "config_dir"}
    if unexpected:
        raise ValueError(f"unknown [host] keys: {sorted(unexpected)}")
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
        unexpected = set(entry) - {"command", "adapter", "cwd", "env", "enabled"}
        if unexpected:
            raise ValueError(f"unknown [agents.{name}] keys: {sorted(unexpected)}")
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
    poll = float(host.get("poll_seconds", 3))
    checkin = float(host.get("checkin_seconds", 60))
    if not math.isfinite(poll) or poll <= 0 or not math.isfinite(checkin) or not 5 <= checkin <= 240:
        raise ValueError("poll_seconds must be positive; checkin_seconds must be 5..240")
    config_dir_value = host.get("config_dir") or os.environ.get("ANX_CONFIG_DIR")
    if not config_dir_value:
        home = os.environ.get("HOME")
        if not home:
            raise ValueError("[host].config_dir or ANX_CONFIG_DIR is required when HOME is absent")
        config_dir_value = str(Path(home) / ".config" / "anx")
    config_dir = Path(config_dir_value).expanduser()
    if not config_dir.is_absolute():
        raise ValueError("host.config_dir must be absolute")
    return Config(path, config_dir.resolve(), base_url, host_id, host_slug, str(host.get("anx", "anx")),
                  str(host.get("agentctl", "agentctl")), poll, checkin, runtimes)
