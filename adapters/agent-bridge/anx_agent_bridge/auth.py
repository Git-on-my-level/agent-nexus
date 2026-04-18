from __future__ import annotations

import base64
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey

from .util import atomic_write_json, read_json_file, utc_now_iso


@dataclass(slots=True)
class AuthState:
    username: str
    agent_id: str
    actor_id: str
    key_id: str
    public_key_b64: str
    private_key_b64: str
    access_token: str = ""
    refresh_token: str = ""
    token_type: str = "Bearer"
    expires_at_epoch: float = 0.0

    def to_dict(self) -> dict[str, Any]:
        return {
            "username": self.username,
            "agent_id": self.agent_id,
            "actor_id": self.actor_id,
            "key_id": self.key_id,
            "public_key_b64": self.public_key_b64,
            "private_key_b64": self.private_key_b64,
            "access_token": self.access_token,
            "refresh_token": self.refresh_token,
            "token_type": self.token_type,
            "expires_at_epoch": self.expires_at_epoch,
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "AuthState":
        return cls(
            username=str(payload.get("username", "")).strip(),
            agent_id=str(payload.get("agent_id", "")).strip(),
            actor_id=str(payload.get("actor_id", "")).strip(),
            key_id=str(payload.get("key_id", "")).strip(),
            public_key_b64=str(payload.get("public_key_b64", "")).strip(),
            private_key_b64=str(payload.get("private_key_b64", "")).strip(),
            access_token=str(payload.get("access_token", "")).strip(),
            refresh_token=str(payload.get("refresh_token", "")).strip(),
            token_type=str(payload.get("token_type", "Bearer")).strip() or "Bearer",
            expires_at_epoch=float(payload.get("expires_at_epoch", 0.0) or 0.0),
        )


class AuthManager:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.state = self._load_state()

    def _load_state(self) -> AuthState | None:
        if not self.path.exists():
            return None
        raw = read_json_file(self.path)
        if not raw:
            return None
        return AuthState.from_dict(raw)

    def save(self) -> None:
        if self.state is None:
            return
        atomic_write_json(self.path, self.state.to_dict())

    @property
    def is_registered(self) -> bool:
        return bool(self.state and self.state.agent_id and self.state.key_id)

    def require_state(self) -> AuthState:
        if self.state is None:
            raise RuntimeError(f"No auth state at {self.path}")
        return self.state

    def generate_keypair(self) -> tuple[str, str]:
        private_key = Ed25519PrivateKey.generate()
        public_key = private_key.public_key()
        private_bytes = private_key.private_bytes(
            encoding=serialization.Encoding.Raw,
            format=serialization.PrivateFormat.Raw,
            encryption_algorithm=serialization.NoEncryption(),
        )
        public_bytes = public_key.public_bytes(
            encoding=serialization.Encoding.Raw,
            format=serialization.PublicFormat.Raw,
        )
        return (
            base64.b64encode(public_bytes).decode("ascii"),
            base64.b64encode(private_bytes).decode("ascii"),
        )

    def register(self, client: "ANXClient", username: str, bootstrap_token: str | None = None, invite_token: str | None = None) -> AuthState:
        public_key_b64, private_key_b64 = self.generate_keypair()
        payload: dict[str, Any] = {
            "username": username,
            "public_key": public_key_b64,
        }
        if bootstrap_token:
            payload["bootstrap_token"] = bootstrap_token
        if invite_token:
            payload["invite_token"] = invite_token
        response = client.raw_request("POST", "/auth/agents/register", json_body=payload, authenticated=False)
        agent = response["agent"]
        key = response["key"]
        tokens = response["tokens"]
        self.state = AuthState(
            username=str(agent["username"]),
            agent_id=str(agent["agent_id"]),
            actor_id=str(agent["actor_id"]),
            key_id=str(key["key_id"]),
            public_key_b64=public_key_b64,
            private_key_b64=private_key_b64,
            access_token=str(tokens["access_token"]),
            refresh_token=str(tokens["refresh_token"]),
            token_type=str(tokens.get("token_type", "Bearer")),
            expires_at_epoch=time.time() + int(tokens.get("expires_in", 0)),
        )
        self.save()
        return self.state

    def assertion_payload(self) -> dict[str, Any]:
        state = self.require_state()
        signed_at = utc_now_iso()
        message = f"anx-auth-token|{state.agent_id}|{state.key_id}|{signed_at}"
        private_bytes = base64.b64decode(state.private_key_b64)
        private_key = Ed25519PrivateKey.from_private_bytes(private_bytes)
        signature = private_key.sign(message.encode("utf-8"))
        return {
            "grant_type": "assertion",
            "agent_id": state.agent_id,
            "key_id": state.key_id,
            "signed_at": signed_at,
            "signature": base64.b64encode(signature).decode("ascii"),
        }

    def has_valid_access_token(self, skew_seconds: int = 30) -> bool:
        return bool(self.state and self.state.access_token and self.state.expires_at_epoch > (time.time() + skew_seconds))

    def refresh(self, client: "ANXClient") -> AuthState:
        state = self.require_state()
        payload: dict[str, Any]
        if state.refresh_token:
            payload = {
                "grant_type": "refresh_token",
                "refresh_token": state.refresh_token,
            }
            try:
                response = client.raw_request("POST", "/auth/token", json_body=payload, authenticated=False)
            except Exception:
                response = client.raw_request("POST", "/auth/token", json_body=self.assertion_payload(), authenticated=False)
        else:
            response = client.raw_request("POST", "/auth/token", json_body=self.assertion_payload(), authenticated=False)
        tokens = response["tokens"]
        state.access_token = str(tokens["access_token"])
        state.refresh_token = str(tokens.get("refresh_token", state.refresh_token))
        state.token_type = str(tokens.get("token_type", state.token_type or "Bearer"))
        state.expires_at_epoch = time.time() + int(tokens.get("expires_in", 0))
        self.save()
        return state

    def access_token(self, client: "ANXClient") -> str:
        if not self.has_valid_access_token():
            self.refresh(client)
        return self.require_state().access_token

    def whoami(self, client: "ANXClient") -> dict[str, Any]:
        return client.raw_request("GET", "/agents/me")


# late import type hint only
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from .anx_client import ANXClient
