import { version } from "$app/environment";
import { writable } from "svelte/store";

const RELOAD_KEY = "anx.chunk-recovery.v2";
export const staleChunkRecoveryNeeded = writable(false);
let attempted = false;

export function isStaleChunkError(error) {
  return /Failed to fetch dynamically imported module|Importing a module script failed|Loading chunk .* failed|error loading dynamically imported module/i.test(
    error?.message || String(error || ""),
  );
}

// Only fixed categories, snapshot age and numeric HTTP status leave these paths.
export function warnOverviewRecovery(kind, updatedAt = null, error = null) {
  const status = error?.status ?? error?.coreHttpStatus;
  console.warn("anx.overview.recovered", {
    kind,
    ageMs: Number.isFinite(updatedAt)
      ? Math.max(0, Date.now() - updatedAt)
      : null,
    status:
      Number.isInteger(status) && status >= 100 && status <= 599
        ? status
        : null,
  });
}

/** One automatic reload per loaded build, including across bootstraps.
 * A newer build gets a new allowance; a broken/cached build cannot loop.
 * Storage denial requires an explicit user retry instead of an unsafe reload.
 */
export function recoverStaleChunk(
  error,
  browser = globalThis.window,
  buildVersion = version,
) {
  if (!browser || !isStaleChunkError(error)) return false;
  const blocked = () => {
    staleChunkRecoveryNeeded.set(true);
    return false;
  };
  if (attempted) return blocked();
  attempted = true;
  if (/(?:^|\/)overview\/?$/.test(browser.location.pathname || ""))
    warnOverviewRecovery("chunk-load", null, error);
  try {
    const build = String(buildVersion);
    if (browser.sessionStorage.getItem(RELOAD_KEY) === build) return blocked();
    browser.sessionStorage.setItem(RELOAD_KEY, build);
  } catch {
    return blocked();
  }
  browser.location.reload();
  return true;
}
