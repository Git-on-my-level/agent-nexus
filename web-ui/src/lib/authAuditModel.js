/**
 * Auth audit events (`GET /auth/audit`) as one readable sentence each, with
 * names instead of principal ids. The ids stay available to the page for a
 * copy affordance; they are never the sentence.
 */

function text(value) {
  return String(value ?? "").trim();
}

/**
 * @param {object} event auth audit event
 * @param {{
 *   nameFor?: (id: string) => string,
 *   hostName?: (hostId: string) => string,
 * }} [options]
 */
export function describeAuthAuditEvent(event, options = {}) {
  const nameFor = options.nameFor ?? (() => "");
  const hostName = options.hostName ?? (() => "");
  const meta =
    event?.metadata && typeof event.metadata === "object" ? event.metadata : {};
  const who = (username, actorId, agentId) =>
    text(nameFor(text(actorId))) ||
    text(nameFor(text(agentId))) ||
    text(username) ||
    "";
  const actor =
    who(event?.actor_username, event?.actor_actor_id, event?.actor_agent_id) ||
    "Someone";
  const subject =
    who(
      event?.subject_username,
      event?.subject_actor_id,
      event?.subject_agent_id,
    ) || "a principal";
  const host =
    text(hostName(text(meta.host_id))) ||
    text(meta.slug) ||
    text(meta.requested_slug) ||
    "a host";
  const agentName = text(meta.name);

  switch (text(event?.event_type)) {
    case "bootstrap_consumed":
      return `${subject} created the workspace`;
    case "principal_registered":
      return `${subject} joined`;
    case "invite_created":
      return `${actor} created an invite`;
    case "invite_consumed":
      return `${subject} joined with an invite`;
    case "invite_revoked":
      return `${actor} revoked an invite`;
    case "principal_revoked":
      return `${actor} revoked ${subject}`;
    case "principal_self_revoked":
      return `${subject} revoked its own access`;
    case "principal_human_lockout_revoked":
      return `${actor} revoked ${subject} under human lockout`;
    case "host_enroll_started":
      return `${host} asked to enroll${meta.requesting_ip ? ` from ${meta.requesting_ip}` : ""}`;
    case "host_enroll_approved":
      return `${actor} approved ${host}`;
    case "host_enroll_denied":
      return `${actor} denied ${host}`;
    case "host_enroll_completed":
      return `${host} enrolled`;
    case "host_agent_adopted":
      return `${subject} moved onto ${host}${agentName ? ` as ${agentName}` : ""}`;
    case "derived_agent_created":
      return agentName
        ? `${agentName} on ${host} used anx for the first time`
        : `A new agent on ${host} used anx for the first time`;
    case "host_exclusions_changed": {
      const names = Array.isArray(meta.excluded_names)
        ? meta.excluded_names.filter(Boolean)
        : [];
      return names.length
        ? `${host} now excludes ${names.join(", ")}`
        : `${host} excludes no names`;
    }
    case "auth_admin_granted":
      return `${actor} granted administration to ${subject}`;
    case "auth_admin_revoked":
      return `${actor} revoked administration from ${subject}`;
    case "host_enrollment_token_created":
      return `${actor} created an enrollment token${meta.label ? ` for ${meta.label}` : ""}`;
    case "host_enrollment_token_revoked":
      return `${actor} revoked an enrollment token`;
    case "host_enrollment_token_consumed": {
      const issuer = who("", meta.issuer_actor_id, meta.issuer_principal_id);
      return issuer
        ? `${host} enrolled using a token issued by ${issuer}`
        : `${host} enrolled using an enrollment token`;
    }
    case "host_revoked":
      return `${actor} revoked ${host} and its agents`;
    case "secret.created":
      return `${actor} created a secret`;
    case "secret.updated":
      return `${actor} updated a secret`;
    case "secret.deleted":
      return `${actor} deleted a secret`;
    case "secret.revealed":
      return `${actor} revealed a secret`;
    default: {
      const kind = text(event?.event_type).replace(/[._]+/g, " ");
      return kind
        ? `${kind.charAt(0).toUpperCase()}${kind.slice(1)}`
        : "Auth event";
    }
  }
}
