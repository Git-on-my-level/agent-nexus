/**
 * Whether this workspace has a PM agent, and what the UI may therefore show.
 *
 * A PM agent runs on the reader's own computer, not on the server, so a
 * workspace can exist for a long time with no PM at all. Until one is
 * onboarded, every PM surface is absent rather than disabled: a button that
 * cannot work is worse than no button.
 *
 * Pure functions only: no network, no stores, no durable state. Core owns the
 * state; this module decides what it means on screen.
 */

/** Computed PM state, as core reports it. */
export const PM_STATES = Object.freeze({
  /** No PM agent has ever connected to this workspace. */
  NOT_ONBOARDED: "not_onboarded",
  /** A PM agent is onboarded and has heartbeated recently. */
  CONNECTED: "connected",
  /** A PM agent is onboarded but is not running right now. */
  OFFLINE: "offline",
  /**
   * Core did not say — the state before the first read returns, and after
   * one fails. It is not evidence either way, so no PM surface is shown and
   * no setup is offered; see `pmFeaturesVisible` and `pmKnownAbsent`.
   */
  UNKNOWN: "unknown",
});

const KNOWN_STATES = new Set([
  PM_STATES.NOT_ONBOARDED,
  PM_STATES.CONNECTED,
  PM_STATES.OFFLINE,
]);

/**
 * Core's code for "this workspace has no PM onboarded".
 *
 * Only this explicit code counts. A 503 `unavailable` is how core answers a
 * PM bridge that is merely down as well as a missing PM identity, and the two
 * cannot be told apart from the response — swallowing it would throw away the
 * reader's draft and tell them to install a PM they already have.
 */
export const PM_NOT_ONBOARDED_CODE = "pm_not_onboarded";

/**
 * Whether a failed request failed because this workspace has no PM.
 *
 * That is an answer — nothing is there — not a fault the reader can act on,
 * and it can arrive before the UI has read the PM state at all.
 *
 * @param {unknown} error
 */
export function isPmNotOnboardedRefusal(error) {
  return String(error?.body?.error?.code ?? "") === PM_NOT_ONBOARDED_CODE;
}

/** A presence record with nothing known in it. */
export const UNKNOWN_PM_PRESENCE = Object.freeze({
  state: PM_STATES.UNKNOWN,
  lastSeen: "",
  runner: "",
  host: "",
});

function text(value) {
  return String(value ?? "").trim();
}

/**
 * Normalize core's PM state payload into the shape the UI reads.
 *
 * Accepts the contract's `{ state, last_seen, runner, host }` and tolerates
 * camelCase, a bare state string, and unknown extra fields. Anything it cannot
 * recognise becomes `unknown`, never `not_onboarded`: guessing "no PM" would
 * hide a working PM.
 *
 * @param {unknown} raw
 * @returns {{ state: string, lastSeen: string, runner: string, host: string }}
 */
export function normalizePmState(raw) {
  if (typeof raw === "string") {
    const state = text(raw).toLowerCase();
    return {
      ...UNKNOWN_PM_PRESENCE,
      state: KNOWN_STATES.has(state) ? state : PM_STATES.UNKNOWN,
    };
  }
  if (!raw || typeof raw !== "object") {
    return { ...UNKNOWN_PM_PRESENCE };
  }
  const state = text(raw.state).toLowerCase();
  return {
    state: KNOWN_STATES.has(state) ? state : PM_STATES.UNKNOWN,
    lastSeen: text(raw.last_seen ?? raw.lastSeen),
    runner: text(raw.runner),
    host: text(raw.host),
  };
}

/**
 * The computed state behind core's `GET /pm/presence` answer (SCA-700).
 *
 * This is the single point of contact with that contract. Core computes the
 * state — `not_onboarded` until a PM has connected at least once, then
 * `connected` or `offline` by heartbeat freshness — and carries the runner and
 * host labels the PM registered with.
 *
 * The `configured` / `connected` booleans are the older shape of the same
 * answer and are read only when `state` is absent, so this UI still works
 * against a core that predates it. There `configured` alone decides: a
 * missing `last_seen_at` is **not** read as "never connected", because that
 * core's presence projection is newer than the PM itself and a workspace that
 * has used its PM for months reports no last-seen the first time it runs —
 * and calling that `not_onboarded` would hide its proposals.
 *
 * @param {Record<string, unknown> | null | undefined} payload
 */
export function pmStateFromPresenceResponse(payload) {
  if (!payload || typeof payload !== "object") {
    return { ...UNKNOWN_PM_PRESENCE };
  }
  const labels = {
    lastSeen: text(payload.last_seen ?? payload.last_seen_at),
    runner: text(payload.runner),
    host: text(payload.host),
  };
  const state = text(payload.state).toLowerCase();
  if (KNOWN_STATES.has(state)) {
    // Core nulls the labels when there is no PM; never carry a stale one.
    return state === PM_STATES.NOT_ONBOARDED
      ? { ...UNKNOWN_PM_PRESENCE, state }
      : { state, ...labels };
  }
  if (payload.configured !== true) {
    return { ...UNKNOWN_PM_PRESENCE, state: PM_STATES.NOT_ONBOARDED };
  }
  return {
    state: payload.connected === true ? PM_STATES.CONNECTED : PM_STATES.OFFLINE,
    ...labels,
  };
}

/**
 * Whether core has answered at all yet.
 *
 * `unknown` is the state before the first read returns and after one fails.
 * Nothing about the PM is shown from it — see `pmFeaturesVisible`.
 *
 * @param {{ state?: string } | null | undefined} presence
 */
export function pmStateKnown(presence) {
  return KNOWN_STATES.has(text(presence?.state));
}

/**
 * Whether PM features may be shown.
 *
 * Only a PM core has positively reported counts: `connected`, or `offline`
 * (it exists, it is just not running). An `unknown` state shows nothing,
 * which covers both the moment before the first read returns and a read that
 * failed — rendering Ask PM on a guess makes it flash in and out on every
 * load, and leaves a button that cannot work when the read never succeeds.
 *
 * This governs what is *displayed*. It is deliberately not the test for
 * whether to attempt a PM write: see `pmKnownAbsent`.
 *
 * @param {{ state?: string } | null | undefined} presence
 */
export function pmFeaturesVisible(presence) {
  const state = text(presence?.state);
  return state === PM_STATES.CONNECTED || state === PM_STATES.OFFLINE;
}

/**
 * Whether core positively says this workspace has no PM.
 *
 * The test for refusing a PM write, and the mirror image of
 * `pmFeaturesVisible` rather than its negation: both are false while the
 * state is unknown. Refusing a write on a guess would turn a slow read into
 * "this workspace has no PM" for a workspace that has one, so an unknown
 * state defers to core, which refuses with `pm_not_onboarded` if it really
 * has none.
 *
 * @param {{ state?: string } | null | undefined} presence
 */
export function pmKnownAbsent(presence) {
  return text(presence?.state) === PM_STATES.NOT_ONBOARDED;
}

/**
 * Whether to offer the setup flow — only once core has said there is no PM,
 * so a slow or failed read never invites the reader to install a second PM
 * alongside one that is already running.
 *
 * @param {{ state?: string } | null | undefined} presence
 */
export function pmSetupOffered(presence) {
  return pmKnownAbsent(presence);
}

/** Whether a PM is onboarded and running right now. */
export function pmConnected(presence) {
  return text(presence?.state) === PM_STATES.CONNECTED;
}

/** Whether a PM is onboarded but not running. */
export function pmOffline(presence) {
  return text(presence?.state) === PM_STATES.OFFLINE;
}

/**
 * The command that installs a PM for this workspace. `--base-url` is included
 * only when the deployment knows its own API origin, matching the host
 * enrollment commands.
 *
 * @param {{ cliBaseUrl?: string }} [options]
 */
export function pmInstallCommand({ cliBaseUrl = "" } = {}) {
  const base = text(cliBaseUrl);
  return `anx ${base ? `--base-url ${base} ` : ""}pm install`;
}

/** The command that reports a PM's local service state. */
export function pmStatusCommand({ cliBaseUrl = "" } = {}) {
  const base = text(cliBaseUrl);
  return `anx ${base ? `--base-url ${base} ` : ""}pm status`;
}

/** The command that removes a PM's local service. */
export function pmUninstallCommand({ cliBaseUrl = "" } = {}) {
  const base = text(cliBaseUrl);
  return `anx ${base ? `--base-url ${base} ` : ""}pm uninstall`;
}

/**
 * Short status line for the PM: what it is doing, and where it runs.
 * Empty when there is nothing true to say.
 *
 * @param {{ state?: string, lastSeen?: string, runner?: string, host?: string } | null} presence
 * @param {number} now
 * @param {(ms: number) => string} formatElapsed
 */
export function pmStatusSummary(presence, now = Date.now(), formatElapsed) {
  const state = text(presence?.state);
  if (state === PM_STATES.CONNECTED) {
    return `PM connected${pmWhereSuffix(presence)}`;
  }
  if (state === PM_STATES.OFFLINE) {
    const since = pmLastSeenLabel(presence, now, formatElapsed);
    return `PM offline${since ? `, last seen ${since} ago` : ""}${pmWhereSuffix(presence)}`;
  }
  return "";
}

/** `, Hermes on studio` / `, on studio` / `` — never a bare separator. */
function pmWhereSuffix(presence) {
  const runner = text(presence?.runner);
  const host = text(presence?.host);
  if (runner && host) return `, ${runner} on ${host}`;
  if (runner) return `, ${runner}`;
  if (host) return `, on ${host}`;
  return "";
}

/**
 * How long ago the PM was last seen (`4m`, `2d 3h`), or "" when the timestamp
 * is missing, unparseable or in the future.
 *
 * @param {{ lastSeen?: string } | null} presence
 * @param {number} now
 * @param {(ms: number) => string} formatElapsed
 */
export function pmLastSeenLabel(presence, now = Date.now(), formatElapsed) {
  const at = Date.parse(text(presence?.lastSeen));
  if (!Number.isFinite(at)) return "";
  const elapsed = now - at;
  if (elapsed < 0) return "";
  return typeof formatElapsed === "function" ? formatElapsed(elapsed) : "";
}
