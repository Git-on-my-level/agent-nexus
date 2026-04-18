from __future__ import annotations

import logging
from typing import Any

import httpx

from ..models import WakePacket
from .base import AdapterResult

LOGGER = logging.getLogger(__name__)


class ZeroClawGatewayAdapter:
    def __init__(
        self,
        *,
        base_url: str,
        bearer_token: str,
        webhook_secret: str | None = None,
        request_timeout_seconds: int = 600,
        session_header_name: str = "X-Session-Id",
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.bearer_token = bearer_token
        self.webhook_secret = webhook_secret or ""
        self.request_timeout_seconds = request_timeout_seconds
        self.session_header_name = session_header_name
        self._http = httpx.Client(base_url=self.base_url, timeout=request_timeout_seconds)

    def doctor(self) -> dict[str, str | int]:
        headers = {"Authorization": f"Bearer {self.bearer_token}"}
        if self.webhook_secret:
            headers["X-Webhook-Secret"] = self.webhook_secret
        try:
            response = self._http.get("/", headers=headers, timeout=min(self.request_timeout_seconds, 10))
        except httpx.HTTPError as exc:
            raise RuntimeError(f"ZeroClaw gateway is not reachable: {exc}") from exc
        if response.status_code >= 500:
            raise RuntimeError(f"ZeroClaw gateway returned {response.status_code} during readiness probe")
        return {
            "adapter_kind": "zeroclaw_gateway",
            "base_url": self.base_url,
            "status_code": response.status_code,
        }

    def dispatch(self, packet: WakePacket, prompt_text: str, session_key: str, existing_native_session_id: str | None = None) -> AdapterResult:
        native_session_id = existing_native_session_id or session_key
        headers = {
            "Authorization": f"Bearer {self.bearer_token}",
            "Content-Type": "application/json",
            self.session_header_name: native_session_id,
            "X-Idempotency-Key": packet.wakeup_id,
        }
        if self.webhook_secret:
            headers["X-Webhook-Secret"] = self.webhook_secret
        response = self._http.post("/webhook", headers=headers, json={"message": prompt_text})
        if response.status_code >= 400:
            raise RuntimeError(f"ZeroClaw webhook failed ({response.status_code}): {response.text}")
        payload: dict[str, Any] = response.json()
        if payload.get("status") == "duplicate":
            LOGGER.info("ZeroClaw treated wakeup %s as duplicate", packet.wakeup_id)
            response_text = ""
        else:
            response_text = str(payload.get("response", "")).strip()
        return AdapterResult(response_text=response_text, native_session_id=native_session_id, metadata=payload)
