/**
 * Who administers this workspace, as one list.
 *
 * Administration reaches the reader two ways and the page has to show both or
 * it lies about who can act. A person holds it implicitly from the moment they
 * join (`principal_kind: "human"`); an agent holds it only from an explicit
 * grant, which is what `GET /auth/admins` returns. People come first: they are
 * the only principals that can change a grant.
 *
 * `granted_at` is not part of the `AuthAdmin` contract, so an agent's grant
 * date is read from the auth audit events the page already loaded. Outside
 * that window the date is simply absent — never guessed from something else.
 */

function text(value) {
  return String(value ?? "").trim();
}

/**
 * Newest `auth_admin_granted` time per principal.
 *
 * Keyed by the subject principal id only. One actor can own several agent
 * principals on different hosts, so an actor id would let one principal's
 * grant date be reported as another's — and core records the principal id on
 * every one of these events, so nothing is lost by ignoring the actor.
 *
 * @param {object[]} auditEvents
 * @returns {Map<string, string>}
 */
export function grantTimesFromAudit(auditEvents = []) {
  const times = new Map();
  for (const event of Array.isArray(auditEvents) ? auditEvents : []) {
    if (text(event?.event_type) !== "auth_admin_granted") continue;
    const at = text(event?.occurred_at);
    const id = text(event?.subject_agent_id);
    if (!at || !id) continue;
    const seen = times.get(id);
    if (!seen || Date.parse(at) > Date.parse(seen)) times.set(id, at);
  }
  return times;
}

/**
 * @param {{
 *   admins?: object[],
 *   principals?: object[],
 *   hosts?: object[],
 *   auditEvents?: object[],
 *   currentPrincipalId?: string,
 *   displayName?: (principal: object) => string,
 * }} input
 * @returns {{
 *   key: string,
 *   principalId: string,
 *   kind: "human" | "agent",
 *   name: string,
 *   handle: string,
 *   hostSlug: string,
 *   grantedAt: string,
 *   isYou: boolean,
 *   revocable: boolean,
 * }[]}
 */
export function buildAdminRows({
  admins = [],
  principals = [],
  hosts = [],
  auditEvents = [],
  currentPrincipalId = "",
  displayName = (principal) => text(principal?.username),
} = {}) {
  const me = text(currentPrincipalId);
  const grantTimes = grantTimesFromAudit(auditEvents);
  const hostSlugByAgentId = new Map();
  for (const host of Array.isArray(hosts) ? hosts : []) {
    for (const agent of host?.agents ?? []) {
      const slug = text(host?.slug) || text(host?.display_name);
      if (text(agent?.id) && slug) hostSlugByAgentId.set(text(agent.id), slug);
    }
  }

  const humans = (Array.isArray(principals) ? principals : [])
    .filter(
      (principal) =>
        principal?.principal_kind === "human" && !principal?.revoked,
    )
    .map((principal) => ({
      key: `human:${text(principal.agent_id)}`,
      principalId: text(principal.agent_id),
      kind: /** @type {"human"} */ ("human"),
      name: displayName(principal) || text(principal.username) || "Unnamed",
      handle: text(principal.username),
      hostSlug: "",
      // A person administers the workspace from the moment they join.
      grantedAt: text(principal.created_at),
      isYou: text(principal.agent_id) === me,
      revocable: false,
    }))
    .sort((a, b) => a.name.localeCompare(b.name));

  const agents = (Array.isArray(admins) ? admins : [])
    .map((admin) => {
      const principalId = text(admin.principal_id);
      const principal = (Array.isArray(principals) ? principals : []).find(
        (entry) => text(entry?.agent_id) === principalId,
      );
      return {
        key: `agent:${principalId}`,
        principalId,
        kind: /** @type {"agent"} */ ("agent"),
        name:
          (principal ? displayName(principal) : "") ||
          text(admin.username) ||
          "Unnamed",
        handle: text(admin.username) || text(principal?.username),
        hostSlug:
          text(admin.host_slug) ||
          hostSlugByAgentId.get(principalId) ||
          text(principal?.host_slug),
        grantedAt: grantTimes.get(principalId) || "",
        isYou: principalId === me,
        revocable: true,
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));

  return [...humans, ...agents];
}

/**
 * Active agent principals that could be granted administration, newest name
 * order, with the ones that already hold it left out.
 *
 * @param {{ principals?: object[], admins?: object[], hosts?: object[] }} input
 */
export function grantCandidates({
  principals = [],
  admins = [],
  hosts = [],
} = {}) {
  const granted = new Set(
    (Array.isArray(admins) ? admins : []).map((admin) =>
      text(admin?.principal_id),
    ),
  );
  const hostSlugByAgentId = new Map();
  for (const host of Array.isArray(hosts) ? hosts : []) {
    for (const agent of host?.agents ?? []) {
      const slug = text(host?.slug) || text(host?.display_name);
      if (text(agent?.id) && slug) hostSlugByAgentId.set(text(agent.id), slug);
    }
  }
  return (Array.isArray(principals) ? principals : [])
    .filter(
      (principal) =>
        principal?.principal_kind === "agent" &&
        !principal?.revoked &&
        text(principal?.agent_id) &&
        !granted.has(text(principal.agent_id)),
    )
    .map((principal) => ({
      principalId: text(principal.agent_id),
      username: text(principal.username),
      hostSlug: hostSlugByAgentId.get(text(principal.agent_id)) || "",
    }))
    .sort((a, b) => a.username.localeCompare(b.username));
}

/**
 * The host whose shared key can request `target`, so the grant confirmation
 * can name what the grant actually trusts.
 *
 * @param {string} target principal id or username typed by the reader
 * @param {{ principals?: object[], hosts?: object[] }} input
 */
export function hostForTarget(target, { principals = [], hosts = [] } = {}) {
  const id = text(target);
  if (!id) return "";
  const principal = (Array.isArray(principals) ? principals : []).find(
    (entry) => text(entry?.agent_id) === id || text(entry?.username) === id,
  );
  const principalId = text(principal?.agent_id) || id;
  for (const host of Array.isArray(hosts) ? hosts : []) {
    const match = (host?.agents ?? []).some(
      (agent) =>
        text(agent?.id) === principalId ||
        text(agent?.handle) === id ||
        text(agent?.id) === id,
    );
    if (match) return text(host?.slug) || text(host?.display_name);
  }
  return "";
}
