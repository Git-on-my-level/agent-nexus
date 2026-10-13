/**
 * One owner format for dense lists.
 *
 * Who owns a row arrives in two shapes a reader never asked to tell apart:
 * a person's registry name, which may carry their role ("Maya Chen (Studio
 * producer)"), and a host-derived agent name, which carries the machine
 * ("priya on dev-host"). In a 10rem column both get cut mid-word by the part
 * nobody scans a table for, and the column ends up with two formats.
 *
 * So a cell shows the name, marks an agent as an agent, and moves the rest —
 * role, host, handle — into the title, where it is one hover away. The
 * stripping is display-only: nothing here renames an actor.
 */

import { buildActorNameMap, isSyntheticAnxHandle } from "./actorSession.js";

/** A trailing parenthetical: a role, a team, a disambiguator. */
const ROLE_SUFFIX = /\s*\(([^()]+)\)\s*$/;
/** A host-derived agent name: `<agent> on <host>`. */
const HOST_RELATION = /^(.*\S)\s+on\s+(\S.*)$/;

/**
 * Split an owner label into the name and the detail that is not the name.
 *
 * `host` gates the host relation deliberately: " on " is a host separator in
 * an agent's derived name and ordinary words in a person's. A human called
 * "Mia Montenegro" keeps every letter.
 *
 * @param {unknown} raw
 * @param {{ host?: boolean }} [options]
 * @returns {{ name: string, role: string, host: string }}
 */
export function splitOwnerLabel(raw, { host = false } = {}) {
  let name = String(raw ?? "").trim();
  let role = "";
  let hostName = "";

  const roleMatch = name.match(ROLE_SUFFIX);
  if (roleMatch && roleMatch.index) {
    role = roleMatch[1].trim();
    name = name.slice(0, roleMatch.index).trim();
  }

  if (host) {
    const hostMatch = name.match(HOST_RELATION);
    if (hostMatch) {
      name = hostMatch[1].trim();
      hostName = hostMatch[2].trim();
    }
  }

  return { name, role, host: hostName };
}

/**
 * Resolve owner ids to one display model per row.
 *
 * Built once per registry change rather than per row: `buildActorNameMap`
 * walks every actor, principal and agent, and a table of a thousand rows
 * calling it a thousand times walked the workspace a thousand times.
 *
 * @param {{
 *   actors?: unknown[],
 *   principals?: unknown[],
 *   agents?: unknown[],
 *   actorsLoaded?: boolean,
 *   agentsLoaded?: boolean,
 * }} [input]
 */
export function buildOwnerDisplay({
  actors = [],
  principals = [],
  agents = [],
  actorsLoaded = false,
  agentsLoaded = false,
} = {}) {
  const names = buildActorNameMap(actors, principals, agents);

  /**
   * Roster entries by every key a row might carry. The derived name is one of
   * them on purpose: `owner` and `next_actor` hold free text as often as an
   * id, and "leo on dev-host" written out by hand is still that agent.
   */
  /** @type {Map<string, object>} */
  const roster = new Map();
  for (const agent of agents ?? []) {
    for (const key of [
      agent?.actor_id,
      agent?.id,
      agent?.handle,
      agent?.display_name,
    ]) {
      const id = String(key ?? "").trim();
      if (id) roster.set(id, agent);
    }
  }

  /** Actors the workspace itself tags as agents, host relation or not. */
  const taggedAgents = new Set();
  for (const actor of actors ?? []) {
    const id = String(actor?.id ?? actor?.actor_id ?? "").trim();
    if (!id) continue;
    const tags = Array.isArray(actor?.tags)
      ? actor.tags.map((tag) => String(tag).toLowerCase())
      : [];
    if (tags.includes("agent")) taggedAgents.add(id);
  }

  /**
   * @param {unknown} owner
   * @returns {null | {
   *   pending: boolean,
   *   name: string,
   *   isAgent: boolean,
   *   title: string,
   *   seed: string,
   * }}
   */
  return function ownerCell(owner) {
    const id = String(owner ?? "")
      .trim()
      .replace(/^actor:/, "");
    if (!id) return null;

    const resolved = String(names.get(id) ?? "").trim();
    const named = Boolean(resolved) && resolved !== id;
    const agent = roster.get(id) ?? null;

    // Nothing names this owner yet, and a read that could is still out.
    // Hold a placeholder rather than printing the id and taking it back.
    if (!actorsLoaded || (!named && !agentsLoaded)) {
      return {
        pending: true,
        name: "",
        isAgent: Boolean(agent),
        title: `Loading who owns this (${id})`,
        seed: id,
      };
    }

    const isAgent = Boolean(agent) || taggedAgents.has(id);
    const label =
      named && !isSyntheticAnxHandle(resolved) ? resolved : resolved || id;
    const parts = splitOwnerLabel(label, { host: isAgent });
    const handle = String(agent?.handle ?? "").trim();

    const detail = [
      parts.role,
      parts.host ? `on ${parts.host}` : "",
      handle && handle !== parts.name ? `@${handle}` : "",
    ].filter(Boolean);

    return {
      pending: false,
      name: parts.name || label || id,
      isAgent,
      title: detail.length
        ? `${parts.name || label} · ${detail.join(" · ")}`
        : parts.name || label,
      seed: id,
    };
  };
}

/**
 * The compact form of an already-resolved label, for the surfaces that hold
 * a name rather than an id (a card's owner line, a doc's author).
 *
 * @param {unknown} label
 * @param {{ isAgent?: boolean }} [options]
 */
export function compactOwnerName(label, { isAgent = false } = {}) {
  const parts = splitOwnerLabel(label, { host: isAgent });
  return parts.name || String(label ?? "").trim();
}
