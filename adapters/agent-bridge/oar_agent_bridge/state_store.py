from __future__ import annotations

import uuid
from pathlib import Path
from typing import Any

from .util import (
    LockedFile,
    SlidingSet,
    atomic_write_json,
    ensure_dir,
    generate_bridge_proof_keypair,
    read_json_file,
)


class JSONStateStore:
    def __init__(self, path: Path, *, ensure_bridge_identity: bool = False) -> None:
        self.path = path
        ensure_dir(path.parent)
        self._guard = LockedFile()
        self._data = read_json_file(path)
        mutated = False
        if ensure_bridge_identity:
            if "bridge_instance_id" not in self._data:
                self._data["bridge_instance_id"] = f"bridge_{uuid.uuid4()}"
                mutated = True
            if "bridge_signing_public_key_spki_b64" not in self._data or "bridge_signing_private_key_pkcs8_b64" not in self._data:
                public_key_b64, private_key_b64 = generate_bridge_proof_keypair()
                self._data["bridge_signing_public_key_spki_b64"] = public_key_b64
                self._data["bridge_signing_private_key_pkcs8_b64"] = private_key_b64
                mutated = True
        if mutated:
            self.flush()

    def get(self, key: str, default: Any = None) -> Any:
        with self._guard:
            return self._data.get(key, default)

    def set(self, key: str, value: Any) -> None:
        with self._guard:
            self._data[key] = value
            self.flush()

    def update(self, updates: dict[str, Any]) -> None:
        with self._guard:
            self._data.update(updates)
            self.flush()

    def flush(self) -> None:
        atomic_write_json(self.path, self._data)

    @property
    def bridge_instance_id(self) -> str:
        return str(self.get("bridge_instance_id", ""))

    @property
    def bridge_signing_public_key_spki_b64(self) -> str:
        return str(self.get("bridge_signing_public_key_spki_b64", ""))

    @property
    def bridge_signing_private_key_pkcs8_b64(self) -> str:
        return str(self.get("bridge_signing_private_key_pkcs8_b64", ""))

    @property
    def last_event_id(self) -> str | None:
        value = self.get("last_event_id")
        return str(value) if value else None

    @last_event_id.setter
    def last_event_id(self, value: str | None) -> None:
        self.set("last_event_id", value)

    def handled_wakeup_ids(self) -> SlidingSet:
        return SlidingSet(self.get("handled_wakeup_ids", []), limit=5000)

    def mark_wakeup_handled(self, wakeup_id: str) -> None:
        handled = self.handled_wakeup_ids()
        handled.add(wakeup_id)
        self.set("handled_wakeup_ids", handled.to_list())

    def session_map(self) -> dict[str, str]:
        raw = self.get("session_map", {})
        if not isinstance(raw, dict):
            return {}
        return {str(k): str(v) for k, v in raw.items()}

    def set_session(self, session_key: str, native_session_id: str) -> None:
        mapping = self.session_map()
        mapping[session_key] = native_session_id
        self.set("session_map", mapping)
