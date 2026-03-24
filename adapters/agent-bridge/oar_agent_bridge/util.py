from __future__ import annotations

import hashlib
import json
import logging
import os
import re
import threading
import time
from pathlib import Path
from typing import Any

LOGGER = logging.getLogger("oar_agent_bridge")


def configure_logging(verbose: bool = False) -> None:
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )


def utc_now_iso() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def ensure_dir(path: Path) -> Path:
    path.mkdir(parents=True, exist_ok=True)
    return path


def atomic_write_json(path: Path, payload: dict[str, Any]) -> None:
    ensure_dir(path.parent)
    temp = path.with_suffix(path.suffix + ".tmp")
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(json.dumps(payload, indent=2, sort_keys=True) + "\n")
        temp.replace(path)
        os.chmod(path, 0o600)
    finally:
        if temp.exists():
            temp.unlink()


def read_json_file(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {}
    return json.loads(path.read_text(encoding="utf-8"))


def sha256_text(*parts: str, length: int | None = None) -> str:
    digest = hashlib.sha256("|".join(parts).encode("utf-8")).hexdigest()
    if length is not None:
        return digest[:length]
    return digest


def stable_json_dumps(value: Any) -> str:
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def parse_bool(value: Any, default: bool = False) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in {"1", "true", "yes", "on"}:
            return True
        if normalized in {"0", "false", "no", "off"}:
            return False
        return default
    return bool(value)


def compact_text(text: str, limit: int = 160) -> str:
    text = re.sub(r"\s+", " ", text or "").strip()
    if len(text) <= limit:
        return text
    return text[: max(0, limit - 1)].rstrip() + "…"


class LockedFile:
    def __init__(self) -> None:
        self._lock = threading.RLock()

    def __enter__(self) -> "LockedFile":
        self._lock.acquire()
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        self._lock.release()


class SlidingSet:
    """Bounded insertion-ordered set persisted as a list."""

    def __init__(self, values: list[str] | None = None, limit: int = 5000) -> None:
        self.limit = max(1, limit)
        self.values = list(dict.fromkeys(values or []))[-self.limit :]

    def add(self, value: str) -> None:
        if value in self.values:
            self.values = [v for v in self.values if v != value]
        self.values.append(value)
        if len(self.values) > self.limit:
            self.values = self.values[-self.limit :]

    def __contains__(self, value: object) -> bool:
        return value in self.values

    def to_list(self) -> list[str]:
        return list(self.values)


def env_default(name: str, default: str | None = None) -> str | None:
    value = os.getenv(name)
    if value is None or value == "":
        return default
    return value
