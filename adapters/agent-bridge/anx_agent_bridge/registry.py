from __future__ import annotations

import logging
from dataclasses import dataclass

from .auth import AuthManager
from .config import LoadedConfig
from .models import AgentBridgeCheckin, AgentRegistration, WorkspaceBinding
from .anx_client import ANXClient
from .util import utc_after_seconds_iso, utc_now_iso, verify_bridge_checkin_signature

LOGGER = logging.getLogger(__name__)


@dataclass(slots=True)
class RegistrationApplyResult:
    agent_id: str
    actor_id: str
    handle: str
    created_or_updated: str
    registration_status: str
    wakeable: bool
    bridge_checked_in_at: str
    bridge_expires_at: str


@dataclass(slots=True)
class RegistrationStatusResult:
    agent_id: str
    handle: str
    actor_id: str
    registration_status: str
    workspace_ids: list[str]
    workspace_bound: bool
    bridge_checked_in_at: str
    bridge_expires_at: str
    wakeable: bool
    blockers: list[str]


def publish_bridge_checkin(
    config: LoadedConfig,
    auth: AuthManager,
    client: ANXClient,
    *,
    bridge_instance_id: str,
    checked_in_at: str,
    expires_at: str,
    proof_signature_b64: str,
) -> None:
    if config.agent is None:
        raise ValueError("bridge check-in requires an [agent] section in config")
    state = auth.require_state()
    checkin = AgentBridgeCheckin(
        handle=config.agent.handle,
        actor_id=state.actor_id,
        workspace_id=config.anx.workspace_id,
        workspace_ids=config.workspace_ids,
        bridge_instance_id=bridge_instance_id,
        checked_in_at=checked_in_at,
        expires_at=expires_at,
        proof_signature_b64=proof_signature_b64,
    )
    client.bridge_check_in(checkin.to_content())


def desired_agent_status(config: LoadedConfig) -> str:
    raw = str(config.agent.status if config.agent is not None else "pending").strip().lower()
    if raw == "disabled":
        return "disabled"
    return "pending"


def effective_registration_status(config: LoadedConfig, checked_in: bool) -> str:
    if desired_agent_status(config) == "disabled":
        return "disabled"
    return "active" if checked_in else "pending"


def apply_registration(
    config: LoadedConfig,
    auth: AuthManager,
    client: ANXClient,
    *,
    bridge_instance_id: str = "",
    bridge_signing_public_key_spki_b64: str = "",
    checked_in: bool = False,
    bridge_checked_in_at: str | None = None,
    bridge_expires_at: str | None = None,
    bridge_proof_signature_b64: str = "",
) -> RegistrationApplyResult:
    if config.agent is None:
        raise ValueError("registration requires an [agent] section in config")
    state = auth.require_state()
    if state.username != config.agent.handle:
        raise ValueError(
            f"auth username {state.username!r} does not match configured agent.handle {config.agent.handle!r}; tags resolve by registered name, so they must match"
        )
    resolved_checked_in_at = bridge_checked_in_at if bridge_checked_in_at is not None else (utc_now_iso() if checked_in else "")
    resolved_expires_at = bridge_expires_at if bridge_expires_at is not None else (utc_after_seconds_iso(config.agent.checkin_ttl_seconds) if checked_in else "")
    registration_status = effective_registration_status(config, checked_in=checked_in)

    registration = AgentRegistration(
        handle=config.agent.handle,
        actor_id=state.actor_id,
        driver_kind=config.agent.driver_kind,
        adapter_kind=config.agent.adapter_kind,
        resume_policy=config.agent.resume_policy,
        status=registration_status,
        workspace_bindings=[WorkspaceBinding(workspace_id=item) for item in config.workspace_ids],
        bridge_instance_id=bridge_instance_id,
        bridge_signing_public_key_spki_b64=bridge_signing_public_key_spki_b64,
        bridge_checked_in_at=resolved_checked_in_at,
        bridge_expires_at=resolved_expires_at,
        bridge_workspace_ids=config.workspace_ids,
        bridge_proof_signature_b64=bridge_proof_signature_b64,
        bridge_checkin_ttl_seconds=config.agent.checkin_ttl_seconds,
    )
    payload = client.patch_current_agent(registration=registration.to_content())
    LOGGER.info("Updated registration metadata for agent %s (@%s)", state.agent_id, config.agent.handle)
    return RegistrationApplyResult(
        agent_id=state.agent_id,
        actor_id=state.actor_id,
        handle=config.agent.handle,
        created_or_updated="updated" if payload is not None else "unknown",
        registration_status=registration.status,
        wakeable=all(registration.supports_workspace(item) for item in config.workspace_ids) and registration.bridge_is_ready(),
        bridge_checked_in_at=registration.bridge_checked_in_at,
        bridge_expires_at=registration.bridge_expires_at,
    )


def registration_status(config: LoadedConfig, auth: AuthManager, client: ANXClient) -> RegistrationStatusResult:
    if config.agent is None:
        raise ValueError("registration status requires an [agent] section in config")
    state = auth.require_state()
    blockers: list[str] = []
    payload = client.get_current_agent()
    agent = payload.get("agent") if isinstance(payload, dict) else None
    registration_payload = agent.get("registration") if isinstance(agent, dict) else None
    if not isinstance(registration_payload, dict):
        return RegistrationStatusResult(
            agent_id=state.agent_id,
            handle=config.agent.handle,
            actor_id=state.actor_id,
            registration_status="missing",
            workspace_ids=config.workspace_ids,
            workspace_bound=False,
            bridge_checked_in_at="",
            bridge_expires_at="",
            wakeable=False,
            blockers=[f"missing registration for agent {state.agent_id}"],
        )
    registration = AgentRegistration.from_content(registration_payload)
    if registration.actor_id != state.actor_id:
        blockers.append("registration actor does not match current auth actor")
    workspace_bound = all(registration.supports_workspace(item) for item in config.workspace_ids)
    if not workspace_bound:
        missing = [item for item in config.workspace_ids if not registration.supports_workspace(item)]
        blockers.append(f"registration is not enabled for workspace(s): {', '.join(missing)}")

    checkin = AgentBridgeCheckin(
        handle=registration.handle,
        actor_id=registration.actor_id,
        workspace_id=config.anx.workspace_id,
        workspace_ids=registration.bridge_workspace_ids or config.workspace_ids,
        bridge_instance_id=registration.bridge_instance_id,
        checked_in_at=registration.bridge_checked_in_at,
        expires_at=registration.bridge_expires_at,
        proof_signature_b64=registration.bridge_proof_signature_b64,
    )
    signature_valid = False
    if registration.bridge_signing_public_key_spki_b64 and registration.bridge_proof_signature_b64:
        signature_valid = verify_bridge_checkin_signature(
            registration.bridge_signing_public_key_spki_b64,
            registration.bridge_proof_signature_b64,
            registration.handle,
            registration.actor_id,
            registration.bridge_workspace_ids or config.workspace_ids,
            registration.bridge_instance_id,
            registration.bridge_checked_in_at,
            registration.bridge_expires_at,
        )
    if registration.status != "active":
        blockers.append("bridge has not checked in yet" if registration.status == "pending" else f"registration status is {registration.status}")
    elif not registration.bridge_signing_public_key_spki_b64:
        blockers.append("registration is missing its bridge signing public key")
    elif not registration.bridge_proof_signature_b64:
        blockers.append("registration is missing its bridge proof signature")
    elif registration.handle and registration.handle != config.agent.handle:
        blockers.append("bridge check-in handle does not match configured handle")
    elif registration.actor_id != state.actor_id:
        blockers.append("bridge check-in actor does not match current auth actor")
    elif not signature_valid:
        blockers.append("bridge check-in signature is invalid")
    elif not all(checkin.is_ready_for_workspace(item) for item in config.workspace_ids):
        blockers.append("bridge check-in is stale")
    return RegistrationStatusResult(
        agent_id=state.agent_id,
        handle=config.agent.handle,
        actor_id=state.actor_id,
        registration_status=registration.status,
        workspace_ids=config.workspace_ids,
        workspace_bound=workspace_bound,
        bridge_checked_in_at=registration.bridge_checked_in_at,
        bridge_expires_at=registration.bridge_expires_at,
        wakeable=registration.actor_id == state.actor_id
        and workspace_bound
        and registration.status == "active"
        and bool(registration.bridge_signing_public_key_spki_b64)
        and bool(registration.bridge_proof_signature_b64)
        and signature_valid
        and all(checkin.is_ready_for_workspace(item) for item in config.workspace_ids),
        blockers=blockers,
    )
