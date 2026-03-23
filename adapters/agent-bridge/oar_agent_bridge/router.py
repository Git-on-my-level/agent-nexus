from __future__ import annotations

import json
import logging
import time
from dataclasses import dataclass
from typing import Any

from .config import LoadedConfig
from .mentions import extract_mentions
from .models import (
    AgentRegistration,
    MESSAGE_POSTED_EVENT,
    WAKE_ARTIFACT_KIND,
    WAKE_REQUEST_EVENT,
    WakePacket,
    registration_document_id,
    wakeup_artifact_id,
    wakeup_request_key,
)
from .oar_client import OARClient, OARClientError
from .state_store import JSONStateStore
from .util import compact_text

LOGGER = logging.getLogger(__name__)


@dataclass(slots=True)
class PrincipalCache:
    loaded_at: float
    principals_by_handle: dict[str, dict[str, Any]]


class WakeRouter:
    def __init__(self, config: LoadedConfig, client: OARClient, state: JSONStateStore) -> None:
        if config.router is None:
            raise ValueError("router config requires a [router] section")
        self.config = config
        self.client = client
        self.state = state
        self._principal_cache = PrincipalCache(loaded_at=0.0, principals_by_handle={})

    def run_forever(self) -> None:
        router_cfg = self.config.router
        assert router_cfg is not None
        while True:
            try:
                last_event_id = self.state.last_event_id
                for stream_message in self.client.stream_events(types=[MESSAGE_POSTED_EVENT], last_event_id=last_event_id):
                    event = self._decode_stream_event(stream_message)
                    if event is None:
                        continue
                    self.handle_message_posted(event)
                    event_id = str(event.get("id", "")).strip()
                    if event_id:
                        self.state.last_event_id = event_id
            except Exception:
                LOGGER.exception("Router stream loop failed; reconnecting")
                time.sleep(router_cfg.reconnect_delay_seconds)

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

    def _load_principals(self, force: bool = False) -> None:
        ttl = self.config.router.principal_cache_ttl_seconds if self.config.router else 60
        if not force and (time.time() - self._principal_cache.loaded_at) < ttl:
            return
        principals = self.client.list_principals(limit=200)
        mapping: dict[str, dict[str, Any]] = {}
        for principal in principals:
            if principal.get("revoked"):
                continue
            if str(principal.get("principal_kind", "")).strip() != "agent":
                continue
            username = str(principal.get("username", "")).strip()
            if username:
                mapping[username] = principal
        self._principal_cache = PrincipalCache(loaded_at=time.time(), principals_by_handle=mapping)

    def handle_message_posted(self, event: dict[str, Any]) -> None:
        text = self.extract_message_text(event)
        handles = extract_mentions(text)
        if not handles:
            return
        thread_id = str(event.get("thread_id", "")).strip()
        event_id = str(event.get("id", "")).strip()
        if not thread_id or not event_id:
            LOGGER.debug("Ignoring message_posted without thread_id or id: %s", event)
            return
        self._load_principals()
        for handle in handles:
            try:
                self._route_mention(handle=handle, event=event, text=text)
            except Exception:
                LOGGER.exception("Failed routing mention @%s from event %s", handle, event_id)
                self._emit_exception(thread_id, event_id, handle, "mention_routing_failed", f"Failed routing @%s" % handle)

    def _route_mention(self, *, handle: str, event: dict[str, Any], text: str) -> None:
        thread_id = str(event["thread_id"])
        event_id = str(event["id"])
        principal = self._principal_cache.principals_by_handle.get(handle)
        if principal is None:
            self._emit_exception(thread_id, event_id, handle, "unknown_agent_handle", f"Unknown tagged agent @{handle}")
            return
        registration = self._load_registration(handle)
        if registration is None:
            self._emit_exception(thread_id, event_id, handle, "missing_agent_registration", f"Tagged agent @{handle} has no registration document")
            return
        if registration.actor_id != str(principal.get("actor_id", "")).strip():
            self._emit_exception(thread_id, event_id, handle, "registration_actor_mismatch", f"Tagged agent @{handle} registration actor does not match principal")
            return
        if not registration.supports_workspace(self.config.oar.workspace_id):
            self._emit_exception(thread_id, event_id, handle, "agent_not_bound_to_workspace", f"Tagged agent @{handle} is not enabled for workspace {self.config.oar.workspace_id}")
            return

        workspace = self.client.get_thread_workspace(thread_id)
        thread = workspace.get("thread") or {}
        wake_artifact_id = wakeup_artifact_id(self.config.oar.workspace_id, thread_id, event_id, registration.actor_id)
        session_key = f"oar:{self.config.oar.workspace_id}:{thread_id}:{handle}"
        packet = WakePacket(
            wakeup_id=wake_artifact_id,
            handle=handle,
            actor_id=registration.actor_id,
            workspace_id=self.config.oar.workspace_id,
            workspace_name=self.config.oar.workspace_name,
            thread_id=thread_id,
            thread_title=str(thread.get("title", thread_id)).strip() or thread_id,
            trigger_event_id=event_id,
            trigger_created_at=str(event.get("ts", "")).strip(),
            trigger_author_actor_id=str(event.get("actor_id", "")).strip(),
            trigger_text=text,
            current_summary=str(thread.get("current_summary", "")).strip(),
            session_key=session_key,
            oar_base_url=self.config.oar.base_url,
            thread_context_url=f"{self.config.oar.base_url}/threads/{thread_id}/context",
            thread_workspace_url=f"{self.config.oar.base_url}/threads/{thread_id}/workspace",
            trigger_event_url=f"{self.config.oar.base_url}/events/{event_id}",
            cli_thread_inspect=f"oar threads inspect --thread-id {thread_id} --json",
            cli_thread_workspace=f"oar threads workspace --thread-id {thread_id} --include-related-event-content --json",
        )
        artifact = {
            "id": wake_artifact_id,
            "kind": WAKE_ARTIFACT_KIND,
            "summary": f"Wake packet for @{handle}",
            "refs": [f"thread:{thread_id}", f"event:{event_id}"],
            "target_handle": handle,
            "target_actor_id": registration.actor_id,
            "workspace_id": self.config.oar.workspace_id,
            "thread_id": thread_id,
        }
        try:
            self.client.create_artifact(artifact=artifact, content=packet.to_content(), content_type="structured")
        except OARClientError as exc:
            if exc.status_code != 409:
                raise
            LOGGER.info("Wake artifact %s already exists; reusing it", wake_artifact_id)
        event_payload = {
            "wakeup_id": wake_artifact_id,
            "wake_artifact_id": wake_artifact_id,
            "target_handle": handle,
            "target_actor_id": registration.actor_id,
            "workspace_id": self.config.oar.workspace_id,
            "workspace_name": self.config.oar.workspace_name,
            "thread_id": thread_id,
            "trigger_event_id": event_id,
            "session_key": session_key,
        }
        self.client.create_event(
            event={
                "type": WAKE_REQUEST_EVENT,
                "thread_id": thread_id,
                "summary": f"Wake requested for @{handle}",
                "refs": [f"thread:{thread_id}", f"event:{event_id}", f"artifact:{wake_artifact_id}"],
                "payload": event_payload,
                "provenance": {"sources": [f"actor_statement:{event_id}"]},
            },
            request_key=wakeup_request_key(self.config.oar.workspace_id, thread_id, event_id, registration.actor_id),
        )
        LOGGER.info("Queued wakeup %s for @%s in thread %s", wake_artifact_id, handle, thread_id)

    def _load_registration(self, handle: str) -> AgentRegistration | None:
        doc_id = registration_document_id(handle)
        try:
            payload = self.client.get_document(doc_id)
        except OARClientError as exc:
            if exc.status_code == 404:
                return None
            raise
        revision = payload.get("revision") or {}
        content = revision.get("content") or {}
        if not isinstance(content, dict):
            return None
        return AgentRegistration.from_content(content)

    def _emit_exception(self, thread_id: str, event_id: str, handle: str, code: str, summary: str) -> None:
        request_key = f"exc-{code}-{handle}-{event_id}"
        self.client.create_event(
            event={
                "type": "exception_raised",
                "thread_id": thread_id,
                "summary": summary,
                "refs": [f"thread:{thread_id}", f"event:{event_id}"],
                "payload": {"subtype": code, "code": code, "handle": handle},
                "provenance": {"sources": [f"actor_statement:{event_id}"]},
            },
            request_key=request_key,
        )

    @staticmethod
    def extract_message_text(event: dict[str, Any]) -> str:
        payload = event.get("payload") or {}
        if isinstance(payload, dict):
            for key in ("text", "message", "body", "content"):
                value = payload.get(key)
                if isinstance(value, str) and value.strip():
                    return value
        body = event.get("body")
        if isinstance(body, str) and body.strip():
            return body
        summary = event.get("summary")
        if isinstance(summary, str):
            return summary
        return ""
