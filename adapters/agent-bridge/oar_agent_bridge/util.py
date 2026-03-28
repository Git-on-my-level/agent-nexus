from __future__ import annotations

import hashlib
import json
import logging
import os
import re
import threading
import time
import base64
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec

LOGGER = logging.getLogger("oar_agent_bridge")


def configure_logging(verbose: bool = False) -> None:
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )


def utc_now_iso() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def utc_now_datetime() -> datetime:
    return datetime.now(UTC)


def utc_after_seconds_iso(seconds: int) -> str:
    seconds = max(0, int(seconds))
    return datetime_to_utc_iso(utc_now_datetime() + timedelta(seconds=seconds))


def datetime_to_utc_iso(value: datetime) -> str:
    return value.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def parse_utc_iso(value: Any) -> datetime | None:
    raw = str(value or "").strip()
    if not raw:
        return None
    try:
        if raw.endswith("Z"):
            return datetime.fromisoformat(raw.replace("Z", "+00:00")).astimezone(UTC)
        return datetime.fromisoformat(raw).astimezone(UTC)
    except ValueError:
        return None


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


def generate_bridge_proof_keypair() -> tuple[str, str]:
    private_key = ec.generate_private_key(ec.SECP256R1())
    public_key = private_key.public_key()
    public_bytes = public_key.public_bytes(
        encoding=serialization.Encoding.DER,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )
    private_bytes = private_key.private_bytes(
        encoding=serialization.Encoding.DER,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    return (
        base64.b64encode(public_bytes).decode("ascii"),
        base64.b64encode(private_bytes).decode("ascii"),
    )


def bridge_checkin_proof_message(
    handle: str,
    actor_id: str,
    workspace_id: str,
    bridge_instance_id: str,
    checked_in_at: str,
    expires_at: str,
) -> bytes:
    return stable_json_dumps(
        {
            "v": "agent-bridge-checkin-proof/v1",
            "handle": handle,
            "actor_id": actor_id,
            "workspace_id": workspace_id,
            "bridge_instance_id": bridge_instance_id,
            "checked_in_at": checked_in_at,
            "expires_at": expires_at,
        }
    ).encode("utf-8")


def sign_bridge_checkin(
    private_key_b64: str,
    handle: str,
    actor_id: str,
    workspace_id: str,
    bridge_instance_id: str,
    checked_in_at: str,
    expires_at: str,
) -> str:
    private_key = serialization.load_der_private_key(base64.b64decode(private_key_b64), password=None)
    if not isinstance(private_key, ec.EllipticCurvePrivateKey):
        raise ValueError("bridge proof private key is not an EC key")
    signature = private_key.sign(
        bridge_checkin_proof_message(handle, actor_id, workspace_id, bridge_instance_id, checked_in_at, expires_at),
        ec.ECDSA(hashes.SHA256()),
    )
    return base64.b64encode(signature).decode("ascii")


def verify_bridge_checkin_signature(
    public_key_b64: str,
    signature_b64: str,
    handle: str,
    actor_id: str,
    workspace_id: str,
    bridge_instance_id: str,
    checked_in_at: str,
    expires_at: str,
) -> bool:
    try:
        public_key = serialization.load_der_public_key(base64.b64decode(public_key_b64))
        if not isinstance(public_key, ec.EllipticCurvePublicKey):
            return False
        public_key.verify(
            base64.b64decode(signature_b64),
            bridge_checkin_proof_message(handle, actor_id, workspace_id, bridge_instance_id, checked_in_at, expires_at),
            ec.ECDSA(hashes.SHA256()),
        )
        return True
    except (ValueError, InvalidSignature):
        return False


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
