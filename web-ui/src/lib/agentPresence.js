import { formatWait } from "$lib/inboxMailbox.js";
import { inboxItemMailboxId } from "$lib/inboxUtils.js";

/**
 * Presentation model for the Agents roster and agent pages.
 *
 * Core derives each agent's state (`GET /agents`); this module only orders,
 * labels and joins what core returns. The roster answers four questions per
 * agent: who (and where), doing what, since when, and whether a human is the
 * blocker. It is a presence surface: a "waiting on you" row links into the
 * Inbox and never answers the ask itself.
 *
 * ## Silence is not an alarm
 *
 * Core reports no state at all for an agent with no signal inside
 * `SignalStaleness` (24h). The roster used to read that absence as **stale**,
 * which put a warning on every agent that simply was not running — a laptop
 * that is closed, a persona nobody has used this week — and made a healthy
 * workspace look like a page of problems.
 *
 * Silence only means something when work is riding on it, so the absence is
 * split three ways and the API is left alone:
 *
 * - **offline** (neutral) — no recent signal, and no work depending on one.
 * - **stale** (warning) — silent past its expectation *while holding* an
 *   in-progress card or an open run. This is the only alarming one, and the
 *   only one that was ever worth a colour.
 * - **inactive** — never checked in at all. An identity that exists and has
 *   never been used is roster bookkeeping, so these collapse into one counted
 *   group instead of a screenful of rows.
 */

/** Derived states in roster order (core precedence, then our split). */
export const AGENT_STATES = [
  { key: "waiting_on_human", label: "Waiting on you", short: "Waiting" },
  { key: "working", label: "Working", short: "Working" },
  { key: "idle", label: "Idle", short: "Idle" },
  { key: "stale", label: "Stale", short: "Stale" },
  { key: "offline", label: "Offline", short: "Offline" },
  {
    key: "inactive",
    label: "Inactive identities",
    short: "Inactive",
    /** Rendered as a counted, collapsed group rather than a list of rows. */
    collapsed: true,
  },
];

const STATE_BY_KEY = new Map(AGENT_STATES.map((state) => [state.key, state]));

/** States that mean "nothing is wrong, nothing is happening". */
const QUIET_STATES = new Set(["offline", "inactive"]);

export function isQuietAgentState(state) {
  return QUIET_STATES.has(text(state));
}

/** Adapters core knows by name; anything else is an operator persona. */
export const KNOWN_ADAPTERS = new Set([
  "claude",
  "codex",
  "cursor",
  "omp",
  "generic",
]);

function text(value) {
  return String(value ?? "").trim();
}

function time(value) {
  const parsed = Date.parse(text(value));
  return Number.isFinite(parsed) ? parsed : Number.NaN;
}

export function agentStateLabel(state) {
  return STATE_BY_KEY.get(text(state))?.label ?? "Unknown";
}

export function agentStateShortLabel(state) {
  return STATE_BY_KEY.get(text(state))?.short ?? "Unknown";
}

/** Route segment for an agent page: the handle reads better than the id. */
export function agentPath(agent) {
  const key = text(agent?.handle) || text(agent?.id);
  return key ? `/agents/${encodeURIComponent(key)}` : "/agents";
}

/** `codex` / `codex gpt-5` / persona name, for the "which runtime" slot. */
export function agentRuntimeLabel(agent) {
  const run = agent?.active_run;
  if (run?.adapter) {
    return [text(run.adapter), text(run.model)].filter(Boolean).join(" ");
  }
  const name = text(agent?.name);
  return KNOWN_ADAPTERS.has(name) ? name : "";
}

/** Adapter or persona, as the header of an agent page names it. */
export function agentKindLabel(agent) {
  const name = text(agent?.name);
  if (!name) return "";
  if (text(agent?.identity_kind) === "standalone") return "Standalone agent";
  return KNOWN_ADAPTERS.has(name) ? "Adapter" : "Persona";
}

/** Short form of a duration in seconds: "14m", "1h 11m", "2d 3h". */
export function formatDurationSeconds(seconds) {
  const value = Number(seconds);
  if (!Number.isFinite(value) || value < 0) return "";
  return formatWait(value * 1000);
}

/** Age of a timestamp in the same units, or "" when unknown. */
export function formatAge(iso, now = Date.now()) {
  const at = time(iso);
  if (!Number.isFinite(at)) return "";
  return formatWait(Math.max(0, now - at));
}

/** When an ask started waiting (`AgentOpenAsk.created_at`), or +∞. */
function askSince(ask) {
  const at = time(ask?.created_at);
  return Number.isFinite(at) ? at : Number.POSITIVE_INFINITY;
}

const ASK_KIND_LABEL = { ask: "Ask", review: "Review", escalate: "Escalation" };

export function askKindLabel(kind) {
  return ASK_KIND_LABEL[text(kind)] ?? "Ask";
}

/**
 * Inbox deep link for an open ask (`AgentOpenAsk.inbox_item_id`), or the
 * Needs you list while core's inbox projection has not caught up.
 */
export function inboxAskPath(ask) {
  const params = new URLSearchParams({ mailbox: "needs-you" });
  const itemId = text(ask?.inbox_item_id);
  const id = itemId ? inboxItemMailboxId({ id: itemId }) : "";
  if (id) params.set("item", id);
  return `/inbox?${params.toString()}`;
}

/**
 * One roster row: what the agent is doing and for how long, in operator
 * terms. A waiting agent names its oldest open ask (`waiting_ask`). Run time
 * is core's `duration_seconds` at `loadedAt`, counted forward to `now`.
 */
/**
 * Does this agent hold something that a silence would hurt?
 *
 * An open run, or a card it has claimed through presence. Core's roster
 * carries `current_card_ref` only while an agent is actually on a card, which
 * is the signal "in progress" means here — the roster does not carry the
 * card's phase, and asking for it per row would be an N+1 on a presence page.
 */
export function agentHoldsWork(agent) {
  return Boolean(agent?.active_run || text(agent?.current_card_ref));
}

/**
 * The state the roster shows, which is core's when core has one.
 *
 * Core's vocabulary is `waiting_on_human | working | idle`, plus an empty
 * state for an agent silent beyond `SignalStaleness`. That silence is the one
 * this splits. A literal `stale` is read as silence too, so a server that
 * starts naming it gets the same three-way split rather than a bare warning
 * on an agent nothing is waiting for.
 */
export function agentPresentationState(agent) {
  const reported = text(agent?.state);
  if (reported && reported !== "stale") return reported;
  if (agentHoldsWork(agent)) return "stale";
  return text(agent?.last_signal_at) ? "offline" : "inactive";
}

export function agentRowModel(
  agent,
  { now = Date.now(), loadedAt = now } = {},
) {
  const state = agentPresentationState(agent);
  const note = text(agent?.last_progress_note);
  const noteAge = formatAge(agent?.last_progress_at, now);
  const cardTitle = text(agent?.current_card_title);
  const cardRef = text(agent?.current_card_ref);
  const run = agent?.active_run ?? null;
  const signalAge = formatAge(agent?.last_signal_at, now);
  const base = {
    state,
    note: note
      ? { text: note, age: noteAge, at: agent?.last_progress_at }
      : null,
    task:
      cardTitle || cardRef
        ? { title: cardTitle || cardRef, ref: cardRef }
        : null,
    ask: null,
    moreAsks: 0,
    headline: "",
    duration: "",
    durationTitle: "",
  };

  if (state === "waiting_on_human") {
    const ask = agent?.waiting_ask ?? null;
    const count = Math.max(Number(agent?.open_asks_count) || 0, ask ? 1 : 0);
    return {
      ...base,
      ask: ask
        ? {
            item: ask,
            title: text(ask.title) || "Open ask",
            kind: askKindLabel(ask.kind),
            severity: text(ask.severity),
            subjectTitle: text(ask.subject_title),
            href: inboxAskPath(ask),
          }
        : null,
      moreAsks: Math.max(0, count - 1),
      headline: ask ? text(ask.title) || "Open ask" : "Waiting on a response",
      duration: ask ? formatAge(ask.created_at, now) : "",
      durationTitle: "Waiting for",
    };
  }

  if (state === "working") {
    return {
      ...base,
      headline: cardTitle || (note ? "" : "Working"),
      duration: run
        ? formatDurationSeconds(
            Number(run.duration_seconds) +
              Math.max(0, Math.floor((now - loadedAt) / 1000)),
          )
        : noteAge || signalAge,
      durationTitle: run ? "Run time" : "Last update",
      run,
    };
  }

  if (state === "idle") {
    return {
      ...base,
      headline: cardTitle || "No current task",
      duration: signalAge,
      durationTitle: "Last signal",
    };
  }

  if (state === "stale") {
    return {
      ...base,
      // The alarming case: something is riding on a silence.
      headline: cardTitle
        ? `No signal for ${signalAge} on this task`
        : `No signal for ${signalAge} during a run`,
      duration: signalAge,
      durationTitle: "Last signal",
    };
  }

  if (state === "inactive") {
    return {
      ...base,
      headline: "Never checked in",
      duration: "",
      durationTitle: "",
    };
  }

  /*
   * Offline: not running, and nothing waiting on it. "Not running" is the
   * whole fact, and it is not a problem — so it reads as a plain statement
   * rather than as a complaint about a missing signal.
   */
  return {
    ...base,
    headline: "Not running",
    duration: signalAge,
    durationTitle: "Last signal",
  };
}

/** Roster grouped by presentation state, in state order; empty groups omitted. */
export function groupAgentsByState(agents = []) {
  const active = (Array.isArray(agents) ? agents : []).filter(
    (agent) => agent && !agent.revoked_at,
  );
  return AGENT_STATES.map((state) => ({
    ...state,
    agents: active
      .filter((agent) => agentPresentationState(agent) === state.key)
      .sort((a, b) => compareWithinState(a, b, state.key)),
  })).filter((group) => group.agents.length > 0);
}

function compareWithinState(a, b, state) {
  if (state === "waiting_on_human") {
    // Longest wait first.
    const diff = askSince(a?.waiting_ask) - askSince(b?.waiting_ask);
    if (diff) return diff;
  }
  if (state === "working") {
    const runA = Number(a?.active_run?.duration_seconds ?? -1);
    const runB = Number(b?.active_run?.duration_seconds ?? -1);
    if (runA !== runB) return runB - runA;
  }
  // Most recent signal first; an agent that never checked in sinks.
  const at = (agent) => {
    const value = time(agent?.last_signal_at);
    return Number.isFinite(value) ? value : Number.NEGATIVE_INFINITY;
  };
  const diff = at(b) - at(a);
  if (diff) return diff;
  return text(a?.display_name).localeCompare(text(b?.display_name));
}

/** Counts for the roster summary line. */
export function rosterSummary(agents = []) {
  const active = (Array.isArray(agents) ? agents : []).filter(
    (agent) => agent && !agent.revoked_at,
  );
  const counts = Object.fromEntries(AGENT_STATES.map((s) => [s.key, 0]));
  for (const agent of active) {
    const state = agentPresentationState(agent);
    if (state in counts) counts[state] += 1;
  }
  /*
   * An identity that has never checked in is not part of "6 agents" in any
   * sense a reader means it: it is a row in a table, counted separately so
   * the headline number describes agents that exist in practice.
   */
  return {
    total: active.length - counts.inactive,
    inactiveTotal: counts.inactive,
    ...counts,
  };
}

/** How many agents are working right now (the nav badge). */
export function workingAgentCount(agents = []) {
  return rosterSummary(agents).working;
}

/** Run state in operator words. */
const RUN_STATE_LABEL = {
  starting: "Starting",
  running: "Running",
  completed: "Completed",
  failed: "Failed",
  cancelled: "Cancelled",
  unknown: "Unknown",
};

export function runStateLabel(state) {
  return RUN_STATE_LABEL[text(state)] ?? "Unknown";
}

/** `exec-…` style label for a run: its launcher id, not the core UUID. */
export function runLabel(run) {
  return text(run?.external_id) || text(run?.handle) || text(run?.id);
}

/** Wall time of a run: start to end, or to now while it is still going. */
export function runDuration(run, now = Date.now()) {
  const start = time(run?.started_at);
  if (!Number.isFinite(start)) return "";
  const endValue = time(run?.ended_at);
  const end = Number.isFinite(endValue) ? endValue : now;
  return formatWait(Math.max(0, end - start));
}

export function isRunActive(run) {
  const state = text(run?.state);
  return (
    (state === "starting" || state === "running" || state === "unknown") &&
    text(run?.liveness) === "alive"
  );
}

/** Task page path for a `card:<slug>` ref. */
export function taskPath(cardRef) {
  const ref = text(cardRef);
  return ref ? `/tasks/${encodeURIComponent(ref)}` : "";
}

/** A readable task title from `card:<slug>` when no title is loaded. */
export function titleFromCardRef(ref, titles = new Map()) {
  const value = text(ref);
  if (!value) return "";
  const known = titles.get(value);
  if (known) return known;
  const slug = value.replace(/^card:/, "");
  if (!slug || /^[0-9a-f-]{32,}$/i.test(slug)) return "";
  const words = slug.replace(/[-_]+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * Progress notes and messages the agent wrote, newest first, as one list.
 * Notes come from presence (`anx work note`); messages are its posts on
 * tasks and threads.
 */
export function agentActivity({ notes = [], messages = [], limit = 20 } = {}) {
  const out = [];
  for (const note of Array.isArray(notes) ? notes : []) {
    if (!text(note?.text)) continue;
    out.push({
      id: `note:${text(note.at)}:${text(note.text).slice(0, 24)}`,
      kind: "note",
      text: text(note.text),
      at: text(note.at),
      cardRef: text(note.card_ref),
      runAttribution: null,
    });
  }
  for (const event of Array.isArray(messages) ? messages : []) {
    const body = text(event?.payload?.text);
    if (!body) continue;
    const cardRef = text(event?.payload?.subject_ref).startsWith("card:")
      ? text(event.payload.subject_ref)
      : ((Array.isArray(event?.refs) ? event.refs : []).find((ref) =>
          text(ref).startsWith("card:"),
        ) ?? "");
    out.push({
      id: `event:${text(event?.id)}`,
      kind: "message",
      text: body,
      at: text(event?.ts),
      cardRef: text(cardRef),
      runAttribution: event?.run_attribution ?? null,
    });
  }
  return out
    .sort((a, b) => (time(b.at) || 0) - (time(a.at) || 0))
    .slice(0, limit);
}
