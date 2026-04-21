/**
 * Structured server-side logger for local dev visibility.
 *
 * In dev (`import.meta.env.DEV` true OR `NODE_ENV !== "production"`) this
 * prints a single-line, easy-to-scan record per event with a stable `[anx]`
 * prefix. In production the logger is silent unless the event level is
 * `error` (so we never lose a real failure to a misconfigured DEV flag).
 *
 * Background: SvelteKit's Vite dev server doesn't log requests by default
 * and hides errors thrown from `load()` behind a generic "Internal Error"
 * page. That makes local debugging extremely opaque. Use these helpers from
 * any server-side module (`+layout.server.js`, `+server.js`, `hooks.server.js`,
 * `lib/server/*`) to surface the failure.
 *
 * Usage:
 *   import { logServerEvent, logServerError } from "$lib/server/devLog";
 *   logServerEvent("workspace.resolve", { org, slug, status: 503 });
 *   logServerError("workspace.resolve.failed", err, { org, slug });
 */

function isDev() {
  try {
    if (typeof import.meta !== "undefined" && import.meta.env?.DEV) {
      return true;
    }
  } catch {
    // import.meta.env may not exist outside of Vite-transformed code.
  }
  if (typeof process !== "undefined") {
    const nodeEnv = String(process.env?.NODE_ENV ?? "").toLowerCase();
    return nodeEnv !== "production";
  }
  return false;
}

function nowIso() {
  return new Date().toISOString();
}

function safeJSON(value) {
  if (value === null || value === undefined) {
    return "";
  }
  if (
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return JSON.stringify(value);
  }
  try {
    return JSON.stringify(value);
  } catch {
    return JSON.stringify(String(value));
  }
}

function formatFields(fields) {
  if (!fields || typeof fields !== "object") {
    return "";
  }
  const parts = [];
  for (const [k, v] of Object.entries(fields)) {
    if (v === undefined) {
      continue;
    }
    parts.push(`${k}=${safeJSON(v)}`);
  }
  return parts.length ? ` ${parts.join(" ")}` : "";
}

/**
 * Log a structured server event. No-op in production unless level === "error".
 *
 * @param {string} event short, dot-separated event name (e.g. "workspace.resolve").
 * @param {object} [fields] structured key/value context attached to the line.
 * @param {{level?: "info"|"warn"|"error"}} [opts]
 */
export function logServerEvent(event, fields = {}, opts = {}) {
  const level = opts.level ?? "info";
  if (!isDev() && level !== "error") {
    return;
  }
  const line = `[anx ${nowIso()}] ${event}${formatFields(fields)}`;
  if (level === "error") {
    console.error(line);
  } else if (level === "warn") {
    console.warn(line);
  } else {
    console.log(line);
  }
}

/**
 * Log a structured server error with the underlying error attached. Always
 * runs in dev and prod (this is for real failures).
 *
 * @param {string} event short, dot-separated event name.
 * @param {unknown} err the underlying error (Error, string, anything).
 * @param {object} [fields] structured key/value context attached to the line.
 */
export function logServerError(event, err, fields = {}) {
  const message = err instanceof Error ? err.message : String(err);
  const stack = err instanceof Error ? err.stack : undefined;
  const status =
    err && typeof err === "object" && "status" in err
      ? Number(/** @type {{status: number}} */ (err).status)
      : undefined;
  const line = `[anx ${nowIso()}] ${event}${formatFields({ ...fields, status, message })}`;
  console.error(line);
  if (isDev() && stack) {
    console.error(stack);
  }
}

// Exported for tests so we don't have to monkey-patch process.env globally.
export const __testing__ = { isDev };
