from __future__ import annotations

from dataclasses import asdict, dataclass, field
from typing import Any

from .util import parse_bool, parse_utc_iso, sha256_text, utc_now_datetime, utc_now_iso

WAKE_PACKET_VERSION = "agent-wake/v1"
REGISTRATION_VERSION = "agent-registration/v1"
BRIDGE_CHECKIN_VERSION = "agent-bridge-checkin/v1"
BRIDGE_CHECKED_IN_EVENT = "agent_bridge_checked_in"
WAKE_ARTIFACT_KIND = "agent_wake"
WAKE_REQUEST_EVENT = "agent_wakeup_requested"
WAKE_CLAIMED_EVENT = "agent_wakeup_claimed"
WAKE_FAILED_EVENT = "agent_wakeup_failed"
WAKE_COMPLETED_EVENT = "agent_wakeup_completed"
MESSAGE_POSTED_EVENT = "message_posted"
DEFAULT_CHECKIN_TTL_SECONDS = 300


@dataclass(slots=True)
class WorkspaceBinding:
    workspace_id: str
    enabled: bool = True

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(slots=True)
class AgentRegistration:
    handle: str
    actor_id: str
    wake_contract_version: str = REGISTRATION_VERSION
    delivery_mode: str = "pull"
    driver_kind: str = "custom"
    resume_policy: str = "resume_or_create"
    status: str = "pending"
    workspace_bindings: list[WorkspaceBinding] = field(default_factory=list)
    adapter_kind: str = "custom"
    bridge_instance_id: str = ""
    bridge_signing_public_key_spki_b64: str = ""
    bridge_checked_in_at: str = ""
    bridge_expires_at: str = ""
    bridge_checkin_event_id: str = ""
    bridge_checkin_ttl_seconds: int = DEFAULT_CHECKIN_TTL_SECONDS
    updated_at: str = field(default_factory=utc_now_iso)

    def to_content(self) -> dict[str, Any]:
        return {
            "version": self.wake_contract_version,
            "handle": self.handle,
            "actor_id": self.actor_id,
            "delivery_mode": self.delivery_mode,
            "driver_kind": self.driver_kind,
            "resume_policy": self.resume_policy,
            "status": self.status,
            "adapter_kind": self.adapter_kind,
            "bridge_instance_id": self.bridge_instance_id,
            "bridge_signing_public_key_spki_b64": self.bridge_signing_public_key_spki_b64,
            "bridge_checked_in_at": self.bridge_checked_in_at,
            "bridge_expires_at": self.bridge_expires_at,
            "bridge_checkin_event_id": self.bridge_checkin_event_id,
            "bridge_checkin_ttl_seconds": self.bridge_checkin_ttl_seconds,
            "updated_at": self.updated_at,
            "workspace_bindings": [binding.to_dict() for binding in self.workspace_bindings],
        }

    @classmethod
    def from_content(cls, content: dict[str, Any]) -> "AgentRegistration":
        bindings = [
            WorkspaceBinding(
                workspace_id=str(item.get("workspace_id", "")).strip(),
                enabled=parse_bool(item.get("enabled", True), default=True),
            )
            for item in (content.get("workspace_bindings") or [])
            if isinstance(item, dict)
        ]
        return cls(
            handle=str(content.get("handle", "")).strip(),
            actor_id=str(content.get("actor_id", "")).strip(),
            wake_contract_version=str(content.get("version", REGISTRATION_VERSION)).strip() or REGISTRATION_VERSION,
            delivery_mode=str(content.get("delivery_mode", "pull")).strip() or "pull",
            driver_kind=str(content.get("driver_kind", "custom")).strip() or "custom",
            resume_policy=str(content.get("resume_policy", "resume_or_create")).strip() or "resume_or_create",
            status=str(content.get("status", "pending")).strip() or "pending",
            workspace_bindings=bindings,
            adapter_kind=str(content.get("adapter_kind", "custom")).strip() or "custom",
            bridge_instance_id=str(content.get("bridge_instance_id", "")).strip(),
            bridge_signing_public_key_spki_b64=str(content.get("bridge_signing_public_key_spki_b64", "")).strip(),
            bridge_checked_in_at=str(content.get("bridge_checked_in_at", "")).strip(),
            bridge_expires_at=str(content.get("bridge_expires_at", "")).strip(),
            bridge_checkin_event_id=str(content.get("bridge_checkin_event_id", "")).strip(),
            bridge_checkin_ttl_seconds=max(0, int(content.get("bridge_checkin_ttl_seconds", DEFAULT_CHECKIN_TTL_SECONDS) or DEFAULT_CHECKIN_TTL_SECONDS)),
            updated_at=str(content.get("updated_at", utc_now_iso())).strip() or utc_now_iso(),
        )

    def supports_workspace(self, workspace_id: str) -> bool:
        wid = workspace_id.strip()
        if not wid:
            return False
        return any(binding.enabled and binding.workspace_id == wid for binding in self.workspace_bindings)

    def bridge_is_ready(self) -> bool:
        if self.status != "active":
            return False
        if not self.bridge_signing_public_key_spki_b64.strip():
            return False
        if not self.bridge_checkin_event_id.strip():
            return False
        if not self.bridge_instance_id.strip():
            return False
        checked_in_at = parse_utc_iso(self.bridge_checked_in_at)
        expires_at = parse_utc_iso(self.bridge_expires_at)
        if checked_in_at is None or expires_at is None:
            return False
        return expires_at >= utc_now_datetime()


@dataclass(slots=True)
class AgentBridgeCheckin:
    handle: str
    actor_id: str
    workspace_id: str
    bridge_instance_id: str
    checked_in_at: str
    expires_at: str
    proof_signature_b64: str = ""
    version: str = BRIDGE_CHECKIN_VERSION
    updated_at: str = field(default_factory=utc_now_iso)

    def to_content(self) -> dict[str, Any]:
        return {
            "version": self.version,
            "handle": self.handle,
            "actor_id": self.actor_id,
            "workspace_id": self.workspace_id,
            "bridge_instance_id": self.bridge_instance_id,
            "checked_in_at": self.checked_in_at,
            "expires_at": self.expires_at,
            "proof_signature_b64": self.proof_signature_b64,
            "updated_at": self.updated_at,
        }

    @classmethod
    def from_content(cls, content: dict[str, Any]) -> "AgentBridgeCheckin":
        return cls(
            handle=str(content.get("handle", "")).strip(),
            actor_id=str(content.get("actor_id", "")).strip(),
            workspace_id=str(content.get("workspace_id", "")).strip(),
            bridge_instance_id=str(content.get("bridge_instance_id", "")).strip(),
            checked_in_at=str(content.get("checked_in_at", "")).strip(),
            expires_at=str(content.get("expires_at", "")).strip(),
            proof_signature_b64=str(content.get("proof_signature_b64", "")).strip(),
            version=str(content.get("version", BRIDGE_CHECKIN_VERSION)).strip() or BRIDGE_CHECKIN_VERSION,
            updated_at=str(content.get("updated_at", utc_now_iso())).strip() or utc_now_iso(),
        )

    def is_ready_for_workspace(self, workspace_id: str) -> bool:
        if self.workspace_id != workspace_id.strip():
            return False
        if not self.bridge_instance_id:
            return False
        checked_in_at = parse_utc_iso(self.checked_in_at)
        expires_at = parse_utc_iso(self.expires_at)
        if checked_in_at is None or expires_at is None:
            return False
        return expires_at >= utc_now_datetime()


@dataclass(slots=True)
class WakePacket:
    wakeup_id: str
    handle: str
    actor_id: str
    workspace_id: str
    workspace_name: str
    thread_id: str
    thread_title: str
    trigger_event_id: str
    trigger_created_at: str
    trigger_author_actor_id: str
    trigger_text: str
    current_summary: str
    session_key: str
    oar_base_url: str
    thread_context_url: str
    thread_workspace_url: str
    trigger_event_url: str
    cli_thread_inspect: str
    cli_thread_workspace: str
    version: str = WAKE_PACKET_VERSION

    def to_content(self) -> dict[str, Any]:
        return {
            "version": self.version,
            "wakeup_id": self.wakeup_id,
            "target": {
                "handle": self.handle,
                "actor_id": self.actor_id,
            },
            "workspace": {
                "id": self.workspace_id,
                "name": self.workspace_name,
            },
            "thread": {
                "id": self.thread_id,
                "title": self.thread_title,
            },
            "trigger": {
                "kind": "mention",
                "message_event_id": self.trigger_event_id,
                "created_at": self.trigger_created_at,
                "author_actor_id": self.trigger_author_actor_id,
                "text": self.trigger_text,
            },
            "context_inline": {
                "current_summary": self.current_summary,
            },
            "session_key": self.session_key,
            "context_fetch": {
                "preferred": "threads.workspace",
                "cli": [self.cli_thread_workspace, self.cli_thread_inspect],
                "api": {
                    "thread": f"{self.oar_base_url.rstrip('/')}/threads/{self.thread_id}",
                    "context": self.thread_context_url,
                    "workspace": self.thread_workspace_url,
                    "trigger_event": self.trigger_event_url,
                },
            },
            "reply_refs": [
                f"thread:{self.thread_id}",
                f"event:{self.trigger_event_id}",
                f"artifact:{self.wakeup_id}",
            ],
        }

    @classmethod
    def from_content(cls, content: dict[str, Any]) -> "WakePacket":
        target = content.get("target") or {}
        workspace = content.get("workspace") or {}
        thread = content.get("thread") or {}
        trigger = content.get("trigger") or {}
        context_inline = content.get("context_inline") or {}
        context_fetch = content.get("context_fetch") or {}
        api = context_fetch.get("api") or {}
        cli = context_fetch.get("cli") or ["", ""]
        base_url = ""
        thread_url = str(api.get("thread", ""))
        if "/threads/" in thread_url:
            base_url = thread_url.split("/threads/", 1)[0]
        return cls(
            wakeup_id=str(content.get("wakeup_id", "")).strip(),
            handle=str(target.get("handle", "")).strip(),
            actor_id=str(target.get("actor_id", "")).strip(),
            workspace_id=str(workspace.get("id", "")).strip(),
            workspace_name=str(workspace.get("name", "")).strip(),
            thread_id=str(thread.get("id", "")).strip(),
            thread_title=str(thread.get("title", "")).strip(),
            trigger_event_id=str(trigger.get("message_event_id", "")).strip(),
            trigger_created_at=str(trigger.get("created_at", "")).strip(),
            trigger_author_actor_id=str(trigger.get("author_actor_id", "")).strip(),
            trigger_text=str(trigger.get("text", "")).strip(),
            current_summary=str(context_inline.get("current_summary", "")).strip(),
            session_key=str(content.get("session_key", "")).strip(),
            oar_base_url=base_url,
            thread_context_url=str(api.get("context", "")).strip(),
            thread_workspace_url=str(api.get("workspace", "")).strip(),
            trigger_event_url=str(api.get("trigger_event", "")).strip(),
            cli_thread_inspect=str(cli[1] if len(cli) > 1 else "").strip(),
            cli_thread_workspace=str(cli[0] if len(cli) > 0 else "").strip(),
            version=str(content.get("version", WAKE_PACKET_VERSION)).strip() or WAKE_PACKET_VERSION,
        )


def registration_document_id(handle: str) -> str:
    return f"agentreg.{handle.strip()}"



def wakeup_request_key(workspace_id: str, thread_id: str, message_event_id: str, actor_id: str) -> str:
    return f"wake-req-{sha256_text(workspace_id, thread_id, message_event_id, actor_id, length=24)}"


def wakeup_artifact_id(workspace_id: str, thread_id: str, message_event_id: str, actor_id: str) -> str:
    return f"wake_{sha256_text(workspace_id, thread_id, message_event_id, actor_id, length=24)}"


def claim_request_key(wakeup_id: str, actor_id: str) -> str:
    return f"wake-claim-{sha256_text(wakeup_id, actor_id, length=24)}"


def completion_request_key(wakeup_id: str, actor_id: str) -> str:
    return f"wake-complete-{sha256_text(wakeup_id, actor_id, length=24)}"


def failure_request_key(wakeup_id: str, actor_id: str) -> str:
    return f"wake-failed-{sha256_text(wakeup_id, actor_id, length=24)}"


def message_request_key(wakeup_id: str, actor_id: str) -> str:
    return f"wake-message-{sha256_text(wakeup_id, actor_id, length=24)}"
