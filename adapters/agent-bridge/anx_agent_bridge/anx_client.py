"""Derived-agent HTTP reads; the CLI owns all host key operations."""
from __future__ import annotations

import json
import subprocess
from typing import Any

import httpx

from .config import Config


class CLIError(RuntimeError):
    pass


class HostCLI:
    def __init__(self, config: Config):
        self.config = config

    def _call(self, *args: str) -> Any:
        result = subprocess.run([self.config.anx, "--json", "--config-dir", str(self.config.config_dir),
                                 "--base-url", self.config.base_url, *args], capture_output=True, text=True, timeout=30)
        try:
            envelope = json.loads(result.stdout)
        except json.JSONDecodeError as exc:
            raise CLIError(f"anx {' '.join(args)} returned invalid JSON: {result.stderr.strip()}") from exc
        if result.returncode or not envelope.get("ok"):
            error = envelope.get("error", {})
            raise CLIError(str(error.get("message") or result.stderr.strip() or "anx command failed"))
        return envelope.get("result", {})

    def token(self, name: str) -> str:
        result = self._call("host", "token", "--as", name)
        agent = result.get("agent") or {}
        if agent.get("handle") != f"{name}.{self.config.host_slug}":
            raise CLIError(f"host token resolved unexpected handle for {name}")
        token = result.get("token")
        if not isinstance(token, str) or not token:
            raise CLIError("host token result is missing token")
        return token

    def checkin(self, instance_id: str) -> Any:
        return self._call("host", "bridge", "check-in", "--host-id", self.config.host_id,
                          "--instance-id", instance_id, "--ttl-seconds", "180")

    def wake(self, action: str, wakeup_id: str, instance_id: str, error: str = "") -> Any:
        if action not in ("claim", "complete", "fail"):
            raise ValueError("invalid wake action")
        args = ["host", "bridge", "wake", action, "--host-id", self.config.host_id,
                "--wakeup-id", wakeup_id, "--instance-id", instance_id]
        if error:
            args.extend(["--error", error[:500]])
        return self._call(*args)


class Client:
    def __init__(self, config: Config, cli: HostCLI):
        self.config = config
        self.cli = cli
        self.http = httpx.Client(base_url=config.base_url, timeout=30)

    def close(self) -> None:
        self.http.close()

    def get(self, name: str, path: str, params: dict[str, Any] | None = None) -> Any:
        response = self.http.get(path, params=params,
                                 headers={"Authorization": f"Bearer {self.cli.token(name)}"})
        response.raise_for_status()
        return response.json()

    def host(self, name: str) -> dict[str, Any]:
        return self.get(name, f"/hosts/{self.config.host_id}")["host"]

    def notifications(self, name: str) -> list[dict[str, Any]]:
        return self.get(name, "/agent-notifications", {"status": "unread,read", "order": "asc"})["items"]

    def wake_packet(self, name: str, wakeup_id: str) -> dict[str, Any]:
        payload = self.get(name, f"/artifacts/{wakeup_id}/content")
        if not isinstance(payload, dict):
            raise ValueError("wake artifact did not contain JSON")
        return payload
