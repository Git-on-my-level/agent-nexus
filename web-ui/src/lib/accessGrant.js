/**
 * An agent's request for a grant, wherever the reader meets it.
 *
 * The same request reaches a person twice: as a row on the Access page and as
 * a review item in the Inbox. Both hand over real authority, so both must say
 * the same thing before they do, and both must send the decisions core
 * accepts. Keeping the facts and the wording here is what stops one surface
 * from quietly becoming the easy way to skip the other's confirmation.
 */

/** Outcomes `POST /inbox/{id}/respond` accepts for an access-backed item. */
export const ACCESS_APPROVE_OUTCOME = "approved";
export const ACCESS_DENY_OUTCOME = "rejected";

function text(value) {
  return String(value ?? "").trim();
}

/**
 * The access facts carried by an inbox item, or null when it is an ordinary
 * review. Core enriches access-backed items with `access_request_id`; nothing
 * else distinguishes them, and guessing from the id or the title would make
 * an ordinary review inherit grant controls.
 *
 * @param {object} item raw inbox item
 */
export function accessRequestFromInboxItem(item) {
  const requestId = text(item?.access_request_id);
  if (!requestId) return null;
  return {
    requestId,
    grant: text(item?.requested_grant),
    requesterPrincipalId: text(item?.requester_principal_id),
    requesterLabel: text(item?.requester_label),
  };
}

/**
 * What approving actually hands over, in one paragraph.
 *
 * `hostSlug` is included only when the caller knows one: an inbox item
 * carries no host, and naming a host that may not exist would make the
 * warning less true, not more complete.
 *
 * @param {{ who?: string, grant?: string, hostSlug?: string }} input
 */
export function describeGrantAuthority({
  who = "This agent",
  grant = "",
  hostSlug = "",
} = {}) {
  const subject = text(who) || "This agent";
  const named = text(grant);
  if (named !== "auth-admin") {
    return `${subject} will be granted ${named || "this authority"}. This grant is audited.`;
  }
  const host = text(hostSlug);
  return [
    `${subject} will be able to decide host enrollments, manage enrollment tokens, revoke other hosts, and read inventory and audit.`,
    "Principal and human invitation revocation still require a person.",
    host
      ? `Granting this agent trusts every process that can read the shared key on ${host} and request this agent name.`
      : "",
    "This grant is audited.",
  ]
    .filter(Boolean)
    .join(" ");
}

/**
 * The question above the confirmation.
 *
 * @param {{ who?: string, grant?: string }} input
 */
export function grantConfirmTitle({ who = "This agent", grant = "" } = {}) {
  const subject = text(who) || "This agent";
  const named = text(grant);
  return named === "auth-admin"
    ? `Let ${subject} administer access?`
    : `Grant ${named || "this authority"} to ${subject}?`;
}
