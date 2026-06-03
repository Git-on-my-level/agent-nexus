from __future__ import annotations

import logging
import threading
import time
from typing import Any

from .auth import AuthManager
from .config import LoadedConfig
from .models import (
    MESSAGE_POSTED_EVENT,
    WakePacket,
    message_request_key,
)
from .anx_client import ANXClient, ANXClientError
from .prompts import build_wake_prompt
from .registry import apply_registration, publish_bridge_checkin
from .state_store import JSONStateStore
from .util import compact_text, sign_bridge_checkin, utc_after_seconds_iso, utc_now_iso

LOGGER = logging.getLogger(__name__)
BRIDGE_RECONNECT_DELAY_SECONDS = 3
NOTIFICATION_READ_RETRY_ATTEMPTS = 3
WAKEUP_FAILURE_RETRY_LIMIT = 3
WAKEUP_REDISPATCH_BACKOFF_BASE_SECONDS = 60
WAKEUP_REDISPATCH_BACKOFF_MAX_SECONDS = 300
REPLY_POST_RETRY_ATTEMPTS = 3
REPLY_POST_RETRY_BASE_SECONDS = 0.5


class AgentBridge:
    def __init__(self, config: LoadedConfig, auth: AuthManager, client: ANXClient, state: JSONStateStore, adapter: Any) -> None:
        if config.agent is None:
            raise ValueError("bridge config requires an [agent] section")
        self.config = config
        self.auth = auth
        self.client = client
        self.state = state
        self.adapter = adapter
        self.handle = config.agent.handle
        # Wakeup ids consumed after adapter dispatch where local handled-state write failed
        # after retries; skip adapter redispatch until restart (set is in-memory only).
        self._completion_without_local_ack: set[str] = set()
        self._failure_counts: dict[str, int] = {}
        self._next_retry_at_monotonic: dict[str, float] = {}

    def run_forever(self) -> None:
        self._start_checkin_loop()
        while True:
            try:
                self._drain_notifications()
                for _ in self.client.stream_agent_notifications(statuses=["unread", "read"]):
                    self._drain_notifications()
            except Exception:
                LOGGER.exception("Bridge loop failed; reconnecting")
                time.sleep(BRIDGE_RECONNECT_DELAY_SECONDS)

    def _start_checkin_loop(self) -> None:
        thread = threading.Thread(target=self._run_checkin_loop, name=f"anx-bridge-checkin-{self.handle}", daemon=True)
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
        # Check-in proves the bridge process is alive and signing keys work; it does not re-probe the adapter.
        # Run `anx bridge doctor` / `anx-agent-bridge bridge doctor` before trusting adapter readiness; a broken
        # adapter can still allow check-ins until the first failing dispatch.
        checked_in_at = utc_now_iso()
        expires_at = utc_after_seconds_iso(self.config.agent.checkin_ttl_seconds)
        proof_signature_b64 = sign_bridge_checkin(
            self.state.bridge_signing_private_key_pkcs8_b64,
            self.handle,
            self.auth.require_state().actor_id,
            self.config.workspace_ids,
            self.state.bridge_instance_id,
            checked_in_at,
            expires_at,
        )
        publish_bridge_checkin(
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
            bridge_checked_in_at=checked_in_at,
            bridge_expires_at=expires_at,
            bridge_proof_signature_b64=proof_signature_b64,
        )

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
        packet: WakePacket | None = None
        try:
            if not wakeup_id or not thread_id:
                raise RuntimeError(f"Malformed agent notification: {notification}")
            if wakeup_id in self._completion_without_local_ack:
                recovered = False
                for attempt in range(5):
                    try:
                        self.state.mark_wakeup_handled(wakeup_id)
                        self._completion_without_local_ack.discard(wakeup_id)
                        recovered = True
                        break
                    except Exception as exc:
                        LOGGER.warning(
                            "Retry mark_wakeup_handled for %s after prior persist failure (attempt %s/5): %s",
                            wakeup_id,
                            attempt + 1,
                            exc,
                        )
                        time.sleep(0.05 * (2**attempt))
                if recovered:
                    try:
                        self._mark_notification_read(wakeup_id)
                    except Exception:
                        LOGGER.exception(
                            "Read-ack for wakeup %s (server-complete, local-state degraded) failed",
                            wakeup_id,
                        )
                return
            if wakeup_id in self.state.handled_wakeup_ids():
                if wakeup_id in self.state.completion_pending_wakeup_ids():
                    try:
                        self.client.complete_agent_wakeup(wakeup_id, self.state.bridge_instance_id)
                        self.state.clear_wakeup_completion_pending(wakeup_id)
                    except Exception as exc:
                        LOGGER.exception("Wakeup %s completion retry failed", wakeup_id)
                        self._record_wakeup_failure_status(
                            None,
                            wakeup_id,
                            target_actor_id,
                            thread_id,
                            request_event_id,
                            exc,
                        )
                        return
                if notification_status != "read":
                    self._mark_notification_read(wakeup_id)
                return
            fail_count = self._failure_counts.get(wakeup_id, 0)
            if fail_count >= WAKEUP_FAILURE_RETRY_LIMIT:
                LOGGER.warning(
                    "Skipping wakeup %s after %d consecutive failures",
                    wakeup_id,
                    fail_count,
                )
                self._mark_wakeup_consumed(wakeup_id)
                self._mark_notification_read(wakeup_id)
                return
            if fail_count > 0 and not self._is_wakeup_retry_due(wakeup_id):
                return
            packet_content = self.client.get_artifact_content(wakeup_id)
            if not isinstance(packet_content, dict):
                raise RuntimeError(f"Wake artifact {wakeup_id} did not return structured content")
            packet = WakePacket.from_content(packet_content)
            claimed = self._claim_wakeup(packet, target_actor_id, request_event_id)
            if not claimed:
                return
        except Exception as exc:
            LOGGER.exception("Wakeup %s failed before adapter dispatch", wakeup_id or "(unknown)")
            if wakeup_id:
                self._record_wakeup_failure(
                    packet,
                    wakeup_id,
                    target_actor_id,
                    thread_id,
                    request_event_id,
                    exc,
                )
            return

        prompt_text = build_wake_prompt(packet)
        session_map = self.state.session_map()
        existing_session_id = session_map.get(packet.session_key)
        pre_dispatch_agent_message_ids = self._agent_message_ids_for_thread(packet)
        try:
            result = self.adapter.dispatch(packet, prompt_text, packet.session_key, existing_native_session_id=existing_session_id)
        except Exception as exc:
            LOGGER.exception("Wakeup %s failed", wakeup_id)
            self._record_wakeup_failure(
                packet,
                wakeup_id,
                target_actor_id,
                thread_id,
                packet.trigger_event_id,
                exc,
            )
            # Per-wakeup failure only. Do not re-raise: that would abort
            # _drain_notifications and run_forever's broad except would log "Bridge loop failed" and
            # skip other pending notifications for the same drain.
            return

        self._failure_counts.pop(packet.wakeup_id, None)
        self._next_retry_at_monotonic.pop(packet.wakeup_id, None)
        if result.native_session_id:
            try:
                self.state.set_session(packet.session_key, result.native_session_id)
            except Exception:
                LOGGER.exception("Wakeup %s: failed to persist native session id", packet.wakeup_id)
        reply_posted = False
        marked_handled = False
        try:
            response_text = result.response_text.strip()
            if self._agent_posted_message_since(packet, pre_dispatch_agent_message_ids):
                LOGGER.info(
                    "Wakeup %s: agent posted a thread response directly; suppressing adapter final-text fallback",
                    packet.wakeup_id,
                )
                reply_posted = True
            elif response_text:
                self._post_reply_message_with_retries(packet, response_text, result.native_session_id)
                reply_posted = True
            if reply_posted:
                marked_handled = self._mark_wakeup_consumed(packet.wakeup_id)
                if marked_handled:
                    self.state.mark_wakeup_completion_pending(packet.wakeup_id)
                else:
                    self._completion_without_local_ack.add(packet.wakeup_id)
            self.client.complete_agent_wakeup(packet.wakeup_id, self.state.bridge_instance_id)
            if marked_handled:
                self.state.clear_wakeup_completion_pending(packet.wakeup_id)
            if not reply_posted and not self._mark_wakeup_consumed(packet.wakeup_id):
                self._completion_without_local_ack.add(packet.wakeup_id)
        except Exception as exc:
            LOGGER.exception("Wakeup %s writeback failed after adapter dispatch", wakeup_id)
            self._record_wakeup_failure_status(
                packet,
                wakeup_id,
                target_actor_id,
                thread_id,
                packet.trigger_event_id,
                exc,
            )
        finally:
            self._mark_notification_read(packet.wakeup_id)

    def _packet_subject_refs(self, packet: WakePacket) -> list[str]:
        return packet.subject_context_refs()

    def _agent_message_ids_for_thread(self, packet: WakePacket) -> set[str] | None:
        if not packet.thread_id or not packet.actor_id:
            return set()
        try:
            return {
                event_id
                for event_id in (
                    str(event.get("id", "")).strip()
                    for event in self.client.list_events(
                        thread_id=packet.thread_id,
                        types=[MESSAGE_POSTED_EVENT],
                        actor_id=packet.actor_id,
                        limit=200,
                    )
                )
                if event_id
            }
        except Exception:
            LOGGER.exception(
                "Wakeup %s: failed to list agent messages for fallback writeback detection",
                packet.wakeup_id,
            )
            return None

    def _agent_posted_message_since(self, packet: WakePacket, before_ids: set[str] | None) -> bool:
        if before_ids is None or not packet.thread_id or not packet.actor_id:
            return False
        current_ids = self._agent_message_ids_for_thread(packet)
        if current_ids is None:
            return False
        return bool(current_ids - before_ids)

    def _record_wakeup_failure(
        self,
        packet: WakePacket | None,
        wakeup_id: str,
        target_actor_id: str,
        thread_id: str,
        event_id: str,
        exc: BaseException,
    ) -> None:
        self._failure_counts[wakeup_id] = self._failure_counts.get(wakeup_id, 0) + 1
        self._schedule_wakeup_retry(wakeup_id, self._failure_counts[wakeup_id])
        self._record_wakeup_failure_status(packet, wakeup_id, target_actor_id, thread_id, event_id, exc)

    def _record_wakeup_failure_status(
        self,
        packet: WakePacket | None,
        wakeup_id: str,
        target_actor_id: str,
        thread_id: str,
        event_id: str,
        exc: BaseException,
    ) -> None:
        try:
            self.client.fail_agent_wakeup(wakeup_id, self.state.bridge_instance_id, str(exc))
        except Exception:
            LOGGER.exception("Wakeup %s failed and failure status write also failed", wakeup_id)

    def _schedule_wakeup_retry(self, wakeup_id: str, fail_count: int) -> None:
        delay = min(
            WAKEUP_REDISPATCH_BACKOFF_BASE_SECONDS * (2 ** (fail_count - 1)),
            WAKEUP_REDISPATCH_BACKOFF_MAX_SECONDS,
        )
        self._next_retry_at_monotonic[wakeup_id] = time.monotonic() + delay
        LOGGER.warning(
            "Scheduled wakeup %s retry in %ss after %d consecutive failures",
            wakeup_id,
            delay,
            fail_count,
        )

    def _is_wakeup_retry_due(self, wakeup_id: str) -> bool:
        retry_at = self._next_retry_at_monotonic.get(wakeup_id, 0)
        now = time.monotonic()
        if retry_at <= now:
            self._next_retry_at_monotonic.pop(wakeup_id, None)
            return True
        LOGGER.info(
            "Skipping wakeup %s until scheduled retry time in %.1fs",
            wakeup_id,
            retry_at - now,
        )
        return False

    def _mark_wakeup_consumed(self, wakeup_id: str) -> bool:
        last_exc: BaseException | None = None
        for attempt in range(5):
            try:
                self.state.mark_wakeup_handled(wakeup_id)
                return True
            except Exception as exc:
                last_exc = exc
                LOGGER.warning(
                    "Wakeup %s: persist local handled state failed (attempt %s/5): %s",
                    wakeup_id,
                    attempt + 1,
                    exc,
                )
                time.sleep(0.05 * (2**attempt))
        if last_exc is not None:
            LOGGER.critical(
                "Wakeup %s: consumed by adapter but local handled state could not be persisted; "
                "skipping adapter redispatch for this id until bridge restart (fix disk/permissions)",
                wakeup_id,
            )
        return False

    def _packet_event_refs(self, packet: WakePacket | None, event_id: str, *, fallback_thread_id: str | None = None, fallback_wakeup_id: str | None = None) -> list[str]:
        if packet is not None:
            refs = self._packet_subject_refs(packet)
            if event_id.strip():
                refs.append(f"event:{event_id}")
            refs.append(f"artifact:{packet.wakeup_id}")
            return refs
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

    def _post_reply_message_with_retries(self, packet: WakePacket, response_text: str, native_session_id: str | None) -> None:
        for attempt in range(1, REPLY_POST_RETRY_ATTEMPTS + 1):
            try:
                self._post_reply_message(packet, response_text, native_session_id)
                return
            except Exception as exc:
                if attempt == REPLY_POST_RETRY_ATTEMPTS:
                    raise
                LOGGER.warning(
                    "Reply post retry %d/%d for wakeup %s failed: %s",
                    attempt,
                    REPLY_POST_RETRY_ATTEMPTS,
                    packet.wakeup_id,
                    exc,
                )
                time.sleep(REPLY_POST_RETRY_BASE_SECONDS * (2 ** (attempt - 1)))

    def _claim_wakeup(self, packet: WakePacket, target_actor_id: str, request_event_id: str) -> bool:
        wakeup_id = packet.wakeup_id
        try:
            response = self.client.claim_agent_wakeup(wakeup_id, self.state.bridge_instance_id)
        except ANXClientError as exc:
            if exc.status_code == 409:
                LOGGER.info("Skipping wakeup %s because another bridge instance already claimed it", wakeup_id)
                return False
            raise
        notification = (response or {}).get("notification") or {}
        owner = str(notification.get("bridge_instance_id", "")).strip()
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
