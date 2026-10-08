import { get, writable } from "svelte/store";

import { coreClient } from "$lib/coreClient";
import {
  PM_STATES,
  UNKNOWN_PM_PRESENCE,
  normalizePmState,
  pmStateFromPresenceResponse,
} from "./onboardingState.js";

/**
 * Whether this workspace has a PM agent, for the whole shell.
 *
 * The PM runs on the reader's own computer, so this is the one fact that
 * decides whether any PM surface exists at all (see `onboardingState.js`).
 * The shell reads it once per workspace; the setup flow polls it while the
 * reader waits for their PM's first heartbeat, and everything else just
 * reacts, so a PM that connects lights the product up without a reload.
 *
 * Per-request cost: one bounded read per refresh — core's PM state is an
 * index lookup on the workspace, with no list, scan or per-item work. The
 * shell refreshes at most once per workspace load plus once a minute when the
 * reader comes back to the tab; the setup page's fast poll runs only while it
 * is open and still waiting.
 */
export const pmPresence = writable(
  /** @type {{ workspace: string, state: string, lastSeen: string, runner: string, host: string, loaded: boolean }} */ ({
    workspace: "",
    ...UNKNOWN_PM_PRESENCE,
    loaded: false,
  }),
);

/** Fast poll while the reader watches for the first heartbeat. */
export const SETUP_POLL_MS = 3_000;
/**
 * How long the setup page watches before it stops and says so.
 *
 * Installing a PM is a few commands on the reader's own machine, so a wait
 * this long means something needs looking at rather than more waiting — and a
 * page left open for a day must not keep spending requests on a hope.
 */
export const SETUP_WATCH_MS = 5 * 60_000;
/** Floor on shell refreshes, so returning to the tab cannot hammer core. */
export const SHELL_REFRESH_MIN_MS = 60_000;

/**
 * Read PM state from core's `GET /pm/presence` (SCA-700).
 *
 * A core without that route cannot be asked, and an unreadable state is not
 * evidence either way, so it answers `unknown` — which shows no PM surface
 * and offers no setup (see `onboardingState.js`).
 *
 * @param {{ client?: object }} [options]
 */
export async function fetchPmState({ client = coreClient } = {}) {
  if (typeof client?.getPmPresence !== "function") {
    return { ...UNKNOWN_PM_PRESENCE };
  }
  return pmStateFromPresenceResponse(await client.getPmPresence());
}

function publish(workspace, presence) {
  pmPresence.set({
    workspace: String(workspace ?? "").trim(),
    state: presence.state,
    lastSeen: presence.lastSeen,
    runner: presence.runner,
    host: presence.host,
    loaded: true,
  });
}

let lastRefreshAt = 0;
let inflight = null;

/**
 * Refresh PM state for `workspace`.
 *
 * Resolves `{ presence, read }`, where `read` is false when the request
 * failed. A failed read never overwrites a state already known, and a stale
 * `connected` is corrected by the next successful read. Callers that retry
 * have to be told the read failed rather than inferring it from `unknown`,
 * which is also the state before the first read returns.
 *
 * Concurrent callers share one request, `force` included: a second request
 * cannot answer sooner than the one already outstanding, and the shared
 * answer is the same one a forced caller wanted.
 *
 * @param {string} workspace
 * @param {{ client?: object, force?: boolean, minIntervalMs?: number }} [options]
 * @returns {Promise<{ presence: object, read: boolean }>}
 */
export async function refreshPmPresence(workspace, options = {}) {
  const key = String(workspace ?? "").trim();
  if (!key) return { presence: get(pmPresence), read: false };
  const { client = coreClient, force = false, minIntervalMs = 0 } = options;

  const current = get(pmPresence);
  if (current.workspace !== key) {
    // A different workspace's answer must not linger while this one loads.
    pmPresence.set({ workspace: key, ...UNKNOWN_PM_PRESENCE, loaded: false });
  } else if (
    !force &&
    minIntervalMs > 0 &&
    current.loaded &&
    Date.now() - lastRefreshAt < minIntervalMs
  ) {
    return { presence: current, read: false };
  }

  if (inflight?.key === key) return inflight.promise;
  const promise = (async () => {
    // Record the attempt, not the success: against an unreachable core a
    // floor that only moves on success is no floor at all.
    lastRefreshAt = Date.now();
    let read = false;
    try {
      const presence = await fetchPmState({ client });
      read = true;
      // Late answer for a workspace the reader has already left.
      if (get(pmPresence).workspace === key) publish(key, presence);
    } catch {
      // Keep whatever is known; an unreadable state is not "no PM".
    } finally {
      if (inflight?.key === key) inflight = null;
    }
    return { presence: get(pmPresence), read };
  })();
  inflight = { key, promise };
  return promise;
}

/**
 * Record a state the caller already has (a send's response, say) without
 * spending a request.
 *
 * @param {string} workspace
 * @param {unknown} raw core's PM state payload
 */
export function publishPmPresence(workspace, raw) {
  const key = String(workspace ?? "").trim();
  if (!key) return;
  const presence = normalizePmState(raw);
  if (presence.state === PM_STATES.UNKNOWN) return;
  lastRefreshAt = Date.now();
  publish(key, presence);
}

/**
 * How many reads that answer nothing usable are tried before giving up. A
 * transient failure or a brief session gap is worth retrying; a core with no
 * presence route will never answer, and polling it forever would be waste.
 */
export const UNREADABLE_POLL_LIMIT = 4;

/**
 * Poll PM state until `until` says to stop (by default: until a PM exists).
 * Returns a stop function.
 *
 * A failed read is not a reason to stop: the reader is watching for their
 * PM's first heartbeat, and giving up on one 401 during session bootstrap
 * would leave the page waiting for something that has already happened.
 *
 * @param {string} workspace
 * @param {{ client?: object, intervalMs?: number, until?: (presence: object) => boolean }} [options]
 */
export function startPmPresencePoll(workspace, options = {}) {
  const key = String(workspace ?? "").trim();
  if (!key) return () => {};
  const {
    client = coreClient,
    intervalMs = SETUP_POLL_MS,
    until = (presence) => presence.state !== PM_STATES.NOT_ONBOARDED,
    /** Watched time, excluding ticks skipped while the tab was hidden. */
    watchMs = SETUP_WATCH_MS,
    /** Called once when the watch gives up, so the surface can say so. */
    onGaveUp = () => {},
  } = options;

  let stopped = false;
  let timer = null;
  let unreadable = 0;
  let watched = 0;

  const giveUp = () => {
    stopped = true;
    onGaveUp();
  };

  const schedule = () => {
    if (stopped) return;
    timer = setTimeout(() => void tick(), intervalMs);
  };
  const tick = async () => {
    if (stopped) return;
    // Nobody is watching a hidden tab; check again on the next tick rather
    // than spending a request per interval in the background.
    if (typeof document !== "undefined" && document.hidden) {
      schedule();
      return;
    }
    const { presence, read } = await refreshPmPresence(key, {
      client,
      force: true,
    });
    if (stopped) return;
    watched += intervalMs;
    if (!read || presence.state === PM_STATES.UNKNOWN) {
      unreadable += 1;
      if (unreadable >= UNREADABLE_POLL_LIMIT) {
        giveUp();
        return;
      }
      schedule();
      return;
    }
    unreadable = 0;
    if (presence.workspace === key && until(presence)) {
      stopped = true;
      return;
    }
    // Nothing arrived in the whole watch window. Stop spending requests and
    // let the surface say what to check instead of waiting silently forever.
    if (watched >= watchMs) {
      giveUp();
      return;
    }
    schedule();
  };

  void tick();
  return () => {
    stopped = true;
    clearTimeout(timer);
  };
}

/** Forget PM state: it belongs to a workspace and a session, not the tab. */
export function clearPmPresence() {
  lastRefreshAt = 0;
  pmPresence.set({ workspace: "", ...UNKNOWN_PM_PRESENCE, loaded: false });
}

/** Test hook. */
export function resetPmPresence() {
  inflight = null;
  clearPmPresence();
}
