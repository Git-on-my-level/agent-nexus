/**
 * Synthetic access data for local Vite dev (`make serve`) when the operator is
 * not signed in, so principals / invites / audit layouts can be QA'd without passkeys.
 */

function isoHoursAgo(hours) {
  return new Date(Date.now() - hours * 3600 * 1000).toISOString();
}

function isoDaysAgo(days) {
  return new Date(Date.now() - days * 86400000).toISOString();
}

/** True only under `vite dev` (not production builds). */
export const isAccessDevPreview =
  typeof import.meta !== "undefined" && import.meta.env?.DEV === true;

/**
 * @returns {{
 *   principals: object[],
 *   invites: object[],
 *   auditEvents: object[],
 *   activeHumanPrincipalCount: number
 * }}
 */
export function getAccessDevMockData() {
  const principals = [
    {
      agent_id: "agent_dev_preview_human_01",
      actor_id: "actor_dev_preview_human_01",
      username: "jordan.ops",
      principal_kind: "human",
      auth_method: "passkey",
      created_at: isoDaysAgo(18),
      last_seen_at: isoHoursAgo(3),
      updated_at: isoHoursAgo(3),
      revoked: false,
      wakeRouting: { applicable: false },
    },
    {
      agent_id: "agent_dev_preview_agent_wake",
      actor_id: "actor_dev_preview_agent_wake",
      username: "hermes.qa",
      principal_kind: "agent",
      auth_method: "public_key",
      created_at: isoDaysAgo(9),
      last_seen_at: isoHoursAgo(36),
      updated_at: isoHoursAgo(36),
      revoked: false,
      wakeRouting: {
        applicable: true,
        badgeLabel: "Wakeable",
        badgeClass: "bg-emerald-500/10 text-emerald-400",
        summary:
          "Dev preview: synthetic wake routing for layout QA (not loaded from core).",
      },
    },
    {
      agent_id: "agent_dev_preview_agent_idle",
      actor_id: "actor_dev_preview_agent_idle",
      username: "batch.worker",
      principal_kind: "agent",
      auth_method: "public_key",
      created_at: isoDaysAgo(4),
      last_seen_at: isoDaysAgo(2),
      updated_at: isoDaysAgo(2),
      revoked: false,
      wakeRouting: {
        applicable: true,
        badgeLabel: "Not wakeable",
        badgeClass: "bg-amber-500/10 text-amber-400",
        summary:
          "Dev preview: agent without a complete registration binding (mock).",
      },
    },
    {
      agent_id: "agent_dev_preview_revoked",
      actor_id: "actor_dev_preview_revoked",
      username: "legacy.bot",
      principal_kind: "agent",
      auth_method: "public_key",
      created_at: isoDaysAgo(60),
      last_seen_at: isoDaysAgo(12),
      updated_at: isoDaysAgo(11),
      revoked: true,
      revoked_at: isoDaysAgo(11),
      wakeRouting: { applicable: false },
    },
  ];

  const invites = [
    {
      id: "invite_dev_preview_pending_01",
      kind: "agent",
      created_by_agent_id: "agent_dev_preview_human_01",
      created_by_actor_id: "actor_dev_preview_human_01",
      created_at: isoHoursAgo(20),
    },
    {
      id: "invite_dev_preview_consumed_01",
      kind: "human",
      created_by_agent_id: "agent_dev_preview_human_01",
      created_by_actor_id: "actor_dev_preview_human_01",
      created_at: isoDaysAgo(5),
      consumed_at: isoDaysAgo(4),
      consumed_by_agent_id: "agent_dev_preview_human_01",
      consumed_by_actor_id: "actor_dev_preview_human_01",
    },
  ];

  const auditEvents = [
    {
      event_id: "audit_dev_preview_01",
      event_type: "principal_registered",
      occurred_at: isoHoursAgo(6),
      metadata: {},
      subject_username: "hermes.qa",
      subject_agent_id: "agent_dev_preview_agent_wake",
    },
    {
      event_id: "audit_dev_preview_02",
      event_type: "invite_created",
      occurred_at: isoDaysAgo(1),
      metadata: {},
      invite_id: "invite_dev_preview_pending_01",
      actor_username: "jordan.ops",
      actor_agent_id: "agent_dev_preview_human_01",
    },
  ];

  return {
    principals,
    invites,
    auditEvents,
    activeHumanPrincipalCount: 1,
  };
}
