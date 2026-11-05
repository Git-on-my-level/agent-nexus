import { KNOWN_ADAPTERS } from "$lib/agentPresence.js";
import { formatAgo } from "$lib/time/format.js";

/**
 * Persona roster model for the Access page.
 *
 * Agents mint personas freely, so a host's roster fills with one-off names.
 * Access shows each host's adapters and its active personas, and folds the
 * rest behind a count. "Active" is per persona: its own last authenticated use
 * (`last_used_at`), its own presence or an open run, within seven days. The
 * host bridge heartbeat never makes a persona active.
 */

export const ACTIVE_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;

/** Page-level filters, in toolbar order. */
export const PERSONA_FILTERS = ["active", "idle", "archived"];

function text(value) {
  return String(value ?? "").trim();
}

function time(value) {
  const parsed = Date.parse(text(value));
  return Number.isFinite(parsed) ? parsed : Number.NaN;
}

/** adapter (always shown), adopted, or persona. */
export function personaKind(agent, host) {
  const name = text(agent?.name);
  if (
    KNOWN_ADAPTERS.has(name) ||
    (host?.discovered_adapters ?? []).includes(name)
  ) {
    return "adapter";
  }
  return text(agent?.identity_kind) === "adopted" ? "adopted" : "persona";
}

/** The persona's own most recent use, as epoch ms, or NaN. */
export function personaLastUsed(agent) {
  const times = [agent?.last_used_at, agent?.last_progress_at]
    .map(time)
    .filter(Number.isFinite);
  return times.length ? Math.max(...times) : Number.NaN;
}

export function isPersonaActive(agent, now = Date.now()) {
  if (!agent || agent.revoked_at || agent.archived_at) return false;
  const state = text(agent.state);
  if (state === "working" || state === "waiting_on_human" || agent.active_run) {
    return true;
  }
  const used = personaLastUsed(agent);
  return Number.isFinite(used) && now - used <= ACTIVE_WINDOW_MS;
}

/**
 * Split one host's agents into rows to show, idle rows to fold, and
 * archived rows. Adapters are always shown unless archived.
 */
export function groupHostPersonas(
  host,
  agents = host?.agents ?? [],
  now = Date.now(),
) {
  const shown = [];
  const idle = [];
  const archived = [];
  for (const agent of agents ?? []) {
    if (!agent) continue;
    if (agent.archived_at) archived.push(agent);
    else if (
      (personaKind(agent, host) === "adapter" && !agent.revoked_at) ||
      isPersonaActive(agent, now)
    )
      shown.push(agent);
    else idle.push(agent);
  }
  const byRecent = (a, b) => {
    const left = personaLastUsed(a);
    const right = personaLastUsed(b);
    const diff =
      (Number.isFinite(right) ? right : -Infinity) -
      (Number.isFinite(left) ? left : -Infinity);
    return diff || text(a.name).localeCompare(text(b.name));
  };
  idle.sort(byRecent);
  archived.sort((a, b) => time(b.archived_at) - time(a.archived_at));
  return { shown, idle, archived };
}

/** "6 hosts · 47 personas · 9 active in the last 7 days" counts. */
export function accessPersonaSummary(hosts = [], now = Date.now()) {
  let personas = 0;
  let active = 0;
  let idle = 0;
  let archived = 0;
  const live = (hosts ?? []).filter((host) => host && !host.revoked_at);
  for (const host of live) {
    const groups = groupHostPersonas(host, host.agents ?? [], now);
    archived += groups.archived.length;
    idle += groups.idle.length;
    for (const agent of [...groups.shown, ...groups.idle]) {
      if (agent.revoked_at) continue;
      personas += 1;
      if (isPersonaActive(agent, now)) active += 1;
    }
  }
  return { hosts: live.length, personas, active, idle, archived };
}

export function accessSummaryLine(summary) {
  const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
  return `${plural(summary.hosts, "host")} · ${plural(summary.personas, "persona")} · ${summary.active} active in the last 7 days`;
}

/** Row subline: when this persona was last used, in its own terms. */
export function personaUseLabel(agent, now = Date.now()) {
  if (agent?.archived_at) {
    const age = formatAgo(agent.archived_at, now);
    const how =
      text(agent.archived_reason) === "inactive" ? "auto-archived" : "archived";
    return age ? `${how} ${age}` : how;
  }
  const used = personaLastUsed(agent);
  if (!Number.isFinite(used)) return "never used";
  return `used ${formatAgo(used, now) || "just now"}`;
}

/** What the folded idle rows have in common, for the "N hidden" line. */
export function idlePersonaRangeLabel(idle = [], now = Date.now()) {
  const used = idle.map(personaLastUsed).filter(Number.isFinite);
  if (!used.length) return "Never used or unused for 7+ days.";
  // formatAgo phrases end in "ago"; a range keeps only the oldest one's.
  const newest = formatAgo(Math.max(...used), now);
  const oldest = formatAgo(Math.min(...used), now);
  if (used.length < idle.length) {
    return `Never used, or last used ${newest} or earlier.`;
  }
  return newest === oldest
    ? `Last used ${newest}.`
    : `Last used ${newest.replace(/ ago$/, "")} to ${oldest}.`;
}

/** Case-insensitive name/handle/host match for the toolbar filter. */
export function personaMatches(agent, host, query) {
  const needle = text(query).toLowerCase();
  if (!needle) return true;
  return [agent?.name, agent?.handle, host?.slug, host?.display_name]
    .map((value) => text(value).toLowerCase())
    .some((value) => value.includes(needle));
}
