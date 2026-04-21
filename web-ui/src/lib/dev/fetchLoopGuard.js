/**
 * Dev-mode fetch loop guard.
 *
 * Wraps `globalThis.fetch` and tracks how often each URL is hit within a short
 * sliding window. If the rate exceeds a threshold (which only happens when a
 * reactive effect, polling loop, or retry loop has gone runaway), throw
 * loudly so the developer sees the bug immediately instead of waiting for the
 * tab to choke on `ERR_INSUFFICIENT_RESOURCES`.
 *
 * This is a *detection* tool, not a *prevention* tool — once you see the
 * thrown error, fix the underlying loop. The guard ships disabled in
 * production builds.
 *
 * Background: we hit this class of bug repeatedly in the SvelteKit shell
 * because any reactive `$effect` that calls a function which writes to one of
 * its own dependent stores will re-fire mid-call. By the time the original
 * fetch resolves the browser has already issued hundreds of duplicates. See
 * `src/lib/authSession.js` (single-flight pattern) for the structural fix
 * and `tests/unit/fetchLoopGuard.test.js` for the contract.
 */

const DEFAULT_WINDOW_MS = 2000;
const DEFAULT_MAX_PER_WINDOW = 25;

/**
 * @typedef {object} LoopGuardOptions
 * @property {number} [windowMs=2000] sliding window size, in ms.
 * @property {number} [maxPerWindow=25] max requests to the same URL in the window.
 * @property {(message: string, info: object) => void} [onTrip] called when the
 *   guard trips. Default logs to console.error and throws. Override in tests.
 * @property {(url: string) => string} [normalize] map a fetch URL to a key for
 *   bucketing (e.g. strip query strings). Defaults to method+pathname.
 */

let installed = false;
/** @type {() => void | null} */
let uninstall = null;

/**
 * Install the guard. Returns an `uninstall` function (mostly used in tests).
 *
 * No-op if already installed.
 *
 * @param {LoopGuardOptions} [options]
 * @returns {() => void}
 */
export function installFetchLoopGuard(options = {}) {
  if (installed && uninstall) {
    return uninstall;
  }

  const windowMs = options.windowMs ?? DEFAULT_WINDOW_MS;
  const maxPerWindow = options.maxPerWindow ?? DEFAULT_MAX_PER_WINDOW;
  const onTrip = options.onTrip ?? defaultOnTrip;
  const normalize = options.normalize ?? defaultNormalize;

  /** @type {Map<string, number[]>} */
  const buckets = new Map();
  /** @type {Set<string>} */
  const tripped = new Set();

  const originalFetch =
    typeof globalThis.fetch === "function" ? globalThis.fetch : null;
  if (!originalFetch) {
    return () => {};
  }

  const wrapped = function loopGuardedFetch(input, init) {
    let key;
    try {
      key = normalize(extractUrl(input), init?.method ?? "GET");
    } catch {
      key = "__unknown__";
    }

    const now =
      typeof performance !== "undefined" && performance.now
        ? performance.now()
        : Date.now();
    const cutoff = now - windowMs;

    let times = buckets.get(key);
    if (!times) {
      times = [];
      buckets.set(key, times);
    }
    // Prune entries outside the window.
    let firstFresh = 0;
    while (firstFresh < times.length && times[firstFresh] < cutoff) {
      firstFresh += 1;
    }
    if (firstFresh > 0) {
      times.splice(0, firstFresh);
    }
    times.push(now);

    if (times.length > maxPerWindow && !tripped.has(key)) {
      tripped.add(key);
      const message =
        `[fetchLoopGuard] runaway request loop detected: ${times.length} ` +
        `requests to "${key}" within ${Math.round(windowMs)}ms ` +
        `(threshold: ${maxPerWindow}). This usually means a Svelte $effect ` +
        `is calling a function that mutates one of its own dependent stores. ` +
        `See src/lib/dev/fetchLoopGuard.js for guidance.`;
      // Wrap in Promise.resolve().then so onTrip's throw becomes a rejected
      // Promise rather than a synchronous throw — fetch's contract is that
      // it always returns a Promise, and code paths that don't await fetch
      // (e.g. `void fetch(...)`) shouldn't blow up synchronously.
      return Promise.resolve().then(() => {
        onTrip(message, {
          key,
          count: times.length,
          windowMs,
          maxPerWindow,
        });
        return originalFetch(input, init);
      });
    }

    return originalFetch(input, init);
  };

  globalThis.fetch = wrapped;
  installed = true;

  uninstall = function uninstallFetchLoopGuard() {
    if (globalThis.fetch === wrapped) {
      globalThis.fetch = originalFetch;
    }
    installed = false;
    uninstall = null;
    buckets.clear();
    tripped.clear();
  };

  return uninstall;
}

function defaultOnTrip(message, info) {
  // Log first so the message survives even if the throw is swallowed by an
  // upstream try/catch (which is depressingly common).
  console.error(message, info);
  const err = new Error(message);
  err.name = "FetchLoopGuardError";
  err.fetchLoopInfo = info;
  throw err;
}

function defaultNormalize(url, method) {
  try {
    const u = new URL(url, "http://_local_");
    return `${String(method || "GET").toUpperCase()} ${u.pathname}`;
  } catch {
    return `${String(method || "GET").toUpperCase()} ${url}`;
  }
}

function extractUrl(input) {
  if (typeof input === "string") return input;
  if (input && typeof input === "object" && "url" in input) {
    return /** @type {Request} */ (input).url;
  }
  if (input instanceof URL) return input.toString();
  return String(input ?? "");
}
