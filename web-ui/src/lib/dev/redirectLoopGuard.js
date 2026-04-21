/**
 * Dev-mode redirect loop guard.
 *
 * Detects ping-pong navigation loops, e.g. when the client hydrates with no
 * authenticated agent, redirects to `/login`, the server-side `+page.server.js`
 * sees a valid workspace cookie and redirects back to `/`, the client hydrates
 * again with no agent, and so on. Without a guard this loop fires hundreds of
 * `goto()` calls per second and saturates the browser with `__data.json`
 * fetches, manifesting only as a generic "Internal Error" page.
 *
 * The guard tracks how often each destination is requested within a sliding
 * window and:
 *   1. Returns `false` from `shouldNavigate(destination)` to short-circuit
 *      the redirect once the threshold is crossed (so the loop actually
 *      stops, instead of just being noisy in the console).
 *   2. Calls the `onTrip` handler exactly once per cooldown period so the
 *      developer gets a single, actionable error in the console rather than
 *      a wall of duplicates.
 *
 * Production builds should never call this — it's pure dev-mode telemetry.
 *
 * Sister utility: `src/lib/dev/fetchLoopGuard.js` (catches the same class of
 * bug at the fetch layer; this guard catches it before the fetch even fires).
 */

const DEFAULT_WINDOW_MS = 2000;
const DEFAULT_MAX_PER_WINDOW = 5;
const DEFAULT_COOLDOWN_MS = 5000;

/**
 * @typedef {object} RedirectLoopGuardOptions
 * @property {number} [windowMs] sliding window size, in ms.
 * @property {number} [maxPerWindow] max navigations to the same destination
 *   in the window before the guard trips.
 * @property {number} [cooldownMs] minimum interval between successive trip
 *   notifications for the same destination (so we don't spam the console).
 * @property {(message: string, info: object) => void} [onTrip] called the
 *   first time the guard trips for a given destination within the cooldown.
 *   Default logs to console.error. Throwing here is allowed but the guard
 *   itself will still suppress the navigation.
 * @property {() => number} [now] injectable clock for tests.
 */

/**
 * @typedef {object} RedirectLoopGuard
 * @property {(destination: string) => boolean} shouldNavigate returns false
 *   when the threshold is crossed for the given destination. Callers should
 *   skip the navigation when this returns false.
 * @property {(destination: string) => void} reset clears state for a single
 *   destination (e.g. on successful login).
 * @property {() => void} resetAll clears all tracking state.
 * @property {(destination: string) => number} count current number of recent
 *   navigations to the destination (for tests/debugging).
 */

/**
 * Create a new redirect loop guard. Each guard maintains its own sliding-
 * window state, so callers can scope guards per redirect intent (e.g. a
 * separate guard for "redirect to /login" vs "redirect after login").
 *
 * @param {RedirectLoopGuardOptions} [options]
 * @returns {RedirectLoopGuard}
 */
export function createRedirectLoopGuard(options = {}) {
  const windowMs = options.windowMs ?? DEFAULT_WINDOW_MS;
  const maxPerWindow = options.maxPerWindow ?? DEFAULT_MAX_PER_WINDOW;
  const cooldownMs = options.cooldownMs ?? DEFAULT_COOLDOWN_MS;
  const onTrip = options.onTrip ?? defaultOnTrip;
  const now = options.now ?? defaultNow;

  /** @type {Map<string, {timestamps: number[], lastTripAt: number}>} */
  const buckets = new Map();

  function pruneBucket(entry, currentTime) {
    const cutoff = currentTime - windowMs;
    while (entry.timestamps.length > 0 && entry.timestamps[0] < cutoff) {
      entry.timestamps.shift();
    }
  }

  function getBucket(destination) {
    let entry = buckets.get(destination);
    if (!entry) {
      // Initialize lastTripAt to -Infinity so the first time we cross the
      // threshold always fires onTrip, regardless of the absolute clock
      // value (matters when callers inject `now: () => 0` in tests).
      entry = { timestamps: [], lastTripAt: Number.NEGATIVE_INFINITY };
      buckets.set(destination, entry);
    }
    return entry;
  }

  return {
    shouldNavigate(destination) {
      const currentTime = now();
      const entry = getBucket(destination);
      pruneBucket(entry, currentTime);
      entry.timestamps.push(currentTime);

      if (entry.timestamps.length <= maxPerWindow) {
        return true;
      }

      // Threshold crossed: emit a single tripping log per cooldown so we
      // don't drown the console, but always suppress the navigation. The
      // navigation will become available again once enough time passes
      // for old timestamps to age out of the window.
      if (currentTime - entry.lastTripAt > cooldownMs) {
        entry.lastTripAt = currentTime;
        const message =
          `[redirectLoopGuard] navigation loop detected: ${entry.timestamps.length} ` +
          `redirects to "${destination}" within ${windowMs}ms ` +
          `(threshold: ${maxPerWindow}). Suppressing further redirects to break ` +
          `the loop. Likely cause: client- and server-side auth state disagree, ` +
          `so the client redirects to /login and the server redirects back. ` +
          `Check $authenticatedAgent hydration in +layout.svelte and the ` +
          `redirect logic in the destination's +page.server.js.`;
        try {
          onTrip(message, {
            destination,
            count: entry.timestamps.length,
            windowMs,
            maxPerWindow,
          });
        } catch {
          // Never let onTrip's throw escape — we still want to suppress the
          // navigation in the caller.
        }
      }
      return false;
    },
    reset(destination) {
      buckets.delete(destination);
    },
    resetAll() {
      buckets.clear();
    },
    count(destination) {
      const entry = buckets.get(destination);
      if (!entry) {
        return 0;
      }
      pruneBucket(entry, now());
      return entry.timestamps.length;
    },
  };
}

function defaultOnTrip(message, info) {
  console.error(message, info);
}

function defaultNow() {
  if (
    typeof performance !== "undefined" &&
    typeof performance.now === "function"
  ) {
    return performance.now();
  }
  return Date.now();
}
