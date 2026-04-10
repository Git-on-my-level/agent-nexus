from __future__ import annotations

import json
import logging
import threading
import time
from typing import Any

from .auth import AuthManager
from .config import LoadedConfig
from .models import (
    MESSAGE_POSTED_EVENT,
    WAKE_CLAIMED_EVENT,
    WAKE_COMPLETED_EVENT,
    WAKE_FAILED_EVENT,
    WAKE_REQUEST_EVENT,
    WakePacket,
    claim_request_key,
    completion_request_key,
    failure_request_key,
    message_request_key,
)
from .oar_client import OARClient, OARClientError, OARStreamDisconnected
from .prompts import build_wake_prompt
from .registry import apply_registration, publish_bridge_checkin
from .state_store import JSONStateStore
from .util import compact_text, sign_bridge_checkin, utc_after_seconds_iso, utc_now_iso

LOGGER = logging.getLogger(__name__)
BRIDGE_RECONNECT_DELAY_SECONDS = 3
NOTIFICATION_READ_RETRY_ATTEMPTS = 3


class AgentBridge:
    def __init__(self, config: LoadedConfig, auth: AuthManager, client: OARClient, state: JSONStateStore, adapter: Any) -> None:
        if config.agent is None:
            raise ValueError("bridge config requires an [agent] section")
        self.config = config
        self.auth = auth
        self.client = client
        self.state = state
        self.adapter = adapter
        self.handle = config.agent.handle

    def run_forever(self) -> None:
        self._start_checkin_loop()
        while True:
            try:
                self._drain_notifications()
                for stream_message in self.client.stream_events(types=[WAKE_REQUEST_EVENT], last_event_id=self.state.last_event_id):
                    event = self._decode_stream_event(stream_message)
                    if event is None:
                        continue
                    event_id = str(event.get("id", "")).strip()
                    payload = event.get("payload") or {}
                    if not self._is_for_me(payload):
                        if event_id:
                            self.state.last_event_id = event_id
                        continue
                    self._drain_notifications()
                    if event_id:
                        self.state.last_event_id = event_id
            except OARStreamDisconnected as exc:
                LOGGER.info("Event stream interrupted; reconnecting: %s", exc)
                time.sleep(BRIDGE_RECONNECT_DELAY_SECONDS)
            except Exception:
                LOGGER.exception("Bridge loop failed; reconnecting")
                time.sleep(BRIDGE_RECONNECT_DELAY_SECONDS)

    def _start_checkin_loop(self) -> None:
        thread = threading.Thread(target=self._run_checkin_loop, name=f"oar-bridge-checkin-{self.handle}", daemon=True)
        thread.start()

    def _run_checkin_loop(self) -> None:
        interval = self.config.agent.checkin_interval_seconds if self.config.agent is not None else 60
        while True:
            try:
                self._publish_checkin()
            except Exception:
                LOGGER.exception("Failed publishing bridge check-in for @%s", self.handle)
            time.sleep(interval)

    def doctor(self) -> dict[str, Any]:
        if hasattr(self.adapter, "doctor"):
            return self.adapter.doctor()
        return {"adapter_kind": type(self.adapter).__name__}

    def _publish_checkin(self) -> None:
        self.doctor()
        checked_in_at = utc_now_iso()
        expires_at = utc_after_seconds_iso(self.config.agent.checkin_ttl_seconds)
        proof_signature_b64 = sign_bridge_checkin(
            self.state.bridge_signing_private_key_pkcs8_b64,
            self.handle,
            self.auth.require_state().actor_id,
            self.config.oar.workspace_id,
            self.state.bridge_instance_id,
            checked_in_at,
            expires_at,
        )
        bridge_checkin_event_id = publish_bridge_checkin(
            self.config,
            self.auth,
            self.client,
            bridge_instance_id=self.state.bridge_instance_id,
            checked_in_at=checked_in_at,
            expires_at=expires_at,
            proof_signature_b64=proof_signature_b64,
        )
        apply_registration(
            self.config,
            self.auth,
            self.client,
            bridge_instance_id=self.state.bridge_instance_id,
            bridge_signing_public_key_spki_b64=self.state.bridge_signing_public_key_spki_b64,
            checked_in=True,
            bridge_checkin_event_id=bridge_checkin_event_id,
            bridge_checked_in_at=checked_in_at,
            bridge_expires_at=expires_at,
        )

    def _decode_stream_event(self, stream_message: dict[str, Any]) -> dict[str, Any] | None:
        data = stream_message.get("data")
        if not isinstance(data, str) or not data.strip():
            return None
        payload = json.loads(data)
        if isinstance(payload, dict) and "event" in payload and isinstance(payload["event"], dict):
            return payload["event"]
        if isinstance(payload, dict):
            return payload
        return None

    def _is_for_me(self, payload: dict[str, Any]) -> bool:
        return str(payload.get("target_handle", "")).strip() == self.handle

    def _drain_notifications(self) -> None:
        notifications = self.client.list_agent_notifications(statuses=["unread", "read"], order="asc")
        for notification in notifications:
            self._handle_notification(notification)

    def _handle_notification(self, notification: dict[str, Any]) -> None:
        wakeup_id = str(notification.get("wakeup_id", "")).strip()
        notification_status = str(notification.get("status", "")).strip().lower()
        target_actor_id = str(notification.get("target_actor_id", "")).strip()
        thread_id = str(notification.get("thread_id", "")).strip()
        request_event_id = str(notification.get("request_event_id", "")).strip()
        if not wakeup_id or not thread_id:
            raise RuntimeError(f"Malformed agent notification: {notification}")
        if wakeup_id in self.state.handled_wakeup_ids():
            if notification_status != "read":
                self._mark_notification_read(wakeup_id)
            return
        packet_content = self.client.get_artifact_content(wakeup_id)
        if not isinstance(packet_content, dict):
            raise RuntimeError(f"Wake artifact {wakeup_id} did not return structured content")
        packet = WakePacket.from_content(packet_content)
        claimed = self._claim_wakeup(packet, target_actor_id, request_event_id)
        if not claimed:
            return
        prompt_text = build_wake_prompt(packet)
        session_map = self.state.session_map()
        existing_session_id = session_map.get(packet.session_key)
        try:
            result = self.adapter.dispatch(packet, prompt_text, packet.session_key, existing_native_session_id=existing_session_id)
            if result.native_session_id:
                self.state.set_session(packet.session_key, result.native_session_id)
            if result.response_text.strip():
                self._post_reply_message(packet, result.response_text.strip(), result.native_session_id)
            self.client.create_event(
                event={
                    "type": WAKE_COMPLETED_EVENT,
                    "thread_id": packet.thread_id,
                    "summary": f"Wakeup {packet.wakeup_id} completed for @{self.handle}",
                    "refs": self._packet_event_refs(packet, packet.trigger_event_id),
                    "payload": {
                        "wakeup_id": packet.wakeup_id,
                        "target_handle": self.handle,
                        "native_session_id": result.native_session_id,
                    },
                    "provenance": {"sources": [f"artifact:{packet.wakeup_id}"]},
                },
                request_key=completion_request_key(packet.wakeup_id, target_actor_id),
            )
        except Exception as exc:
            LOGGER.exception("Wakeup %s failed", wakeup_id)
            self.client.create_event(
                event={
                    "type": WAKE_FAILED_EVENT,
                    "thread_id": thread_id,
                    "summary": f"Wakeup {wakeup_id} failed for @{self.handle}",
                    "refs": self._packet_event_refs(packet, packet.trigger_event_id),
                    "payload": {
                        "wakeup_id": wakeup_id,
                        "target_handle": self.handle,
                        "error": str(exc),
                    },
                    "provenance": {"sources": [f"artifact:{wakeup_id}"]},
                },
                request_key=failure_request_key(wakeup_id, target_actor_id),
            )
            raise
        self.state.mark_wakeup_handled(packet.wakeup_id)
        self._mark_notification_read(packet.wakeup_id)

    def _packet_subject_refs(self, packet: WakePacket) -> list[str]:
        return packet.subject_context_refs()

    def _packet_event_refs(self, packet: WakePacket | None, event_id: str, *, fallback_thread_id: str | None = None, fallback_wakeup_id: str | None = None) -> list[str]:
        if packet is not None:
            refs = self._packet_subject_refs(packet)
            return [*refs, f"event:{event_id}", f"artifact:{packet.wakeup_id}"]
        refs = []
        if fallback_thread_id:
            refs.append(f"thread:{fallback_thread_id}")
        if event_id.strip():
            refs.append(f"event:{event_id}")
        if fallback_wakeup_id:
            refs.append(f"artifact:{fallback_wakeup_id}")
        return refs

    def _mark_notification_read(self, wakeup_id: str) -> None:
        for attempt in range(1, NOTIFICATION_READ_RETRY_ATTEMPTS + 1):
            try:
                self.client.mark_agent_notification_read(wakeup_id)
                return
            except Exception as exc:
                if attempt == NOTIFICATION_READ_RETRY_ATTEMPTS:
                    LOGGER.error(
                        "Wakeup %s completed but notification read acknowledgement failed after %d attempts: %s",
                        wakeup_id,
                        attempt,
                        exc,
                    )
                    return
                LOGGER.warning(
                    "Failed marking notification %s read (attempt %d/%d): %s",
                    wakeup_id,
                    attempt,
                    NOTIFICATION_READ_RETRY_ATTEMPTS,
                    exc,
                )
                time.sleep(BRIDGE_RECONNECT_DELAY_SECONDS)

    def _claim_wakeup(self, packet: WakePacket, target_actor_id: str, request_event_id: str) -> bool:
        wakeup_id = packet.wakeup_id
        thread_id = packet.thread_id
        try:
            claim_refs = [*self._packet_subject_refs(packet), f"event:{request_event_id}", f"artifact:{wakeup_id}"]
            response = self.client.create_event(
                event={
                    "type": WAKE_CLAIMED_EVENT,
                    "thread_id": thread_id,
                    "summary": f"Wakeup {wakeup_id} claimed by @{self.handle}",
                    "refs": claim_refs,
                    "payload": {
                        "wakeup_id": wakeup_id,
                        "target_handle": self.handle,
                        "bridge_instance_id": self.state.bridge_instance_id,
                    },
                    "provenance": {"sources": [f"artifact:{wakeup_id}"]},
                },
                request_key=claim_request_key(wakeup_id, target_actor_id or self.handle),
            )
        except OARClientError as exc:
            if exc.status_code == 409:
                LOGGER.info("Skipping wakeup %s because another bridge instance already claimed it", wakeup_id)
                return False
            raise
        event_payload = ((response or {}).get("event") or {}).get("payload") or {}
        owner = str(event_payload.get("bridge_instance_id", "")).strip()
        if owner and owner != self.state.bridge_instance_id:
            LOGGER.info("Skipping wakeup %s because another bridge instance claimed it: %s", wakeup_id, owner)
            return False
        return True

    def _post_reply_message(self, packet: WakePacket, response_text: str, native_session_id: str | None) -> None:
        self.client.create_event(
            event={
                "type": MESSAGE_POSTED_EVENT,
                "thread_id": packet.thread_id,
                "summary": compact_text(response_text, 140) or f"@{self.handle} replied",
                "refs": self._packet_event_refs(packet, packet.trigger_event_id),
                "payload": {
                    "text": response_text,
                    "agent_handle": self.handle,
                    "wakeup_id": packet.wakeup_id,
                    "native_session_id": native_session_id,
                },
                "provenance": {"sources": [f"artifact:{packet.wakeup_id}"]},
            },
            request_key=message_request_key(packet.wakeup_id, self.handle),
        )
