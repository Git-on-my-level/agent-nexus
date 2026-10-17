import { version } from "$app/environment";
import { writable } from "svelte/store";

const RELOAD_KEY = "anx.chunk-recovery.v2";
const BUDGET_KEY = "anx.chunk-recovery-budget.v1";
const RELOAD_WINDOW_MS = 60_000;
const MAX_WINDOW_RELOADS = 2;
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
 * A newer build gets a new allowance. A shared two-reload/minute budget
 * also stops alternating broken builds; further failures need manual Retry.
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
    const now = Date.now();
    const stored = browser.sessionStorage.getItem(BUDGET_KEY);
    let budget = stored ? JSON.parse(stored) : { at: now, count: 0 };
    if (
      !Number.isFinite(budget?.at) ||
      !Number.isInteger(budget?.count) ||
      budget.count < 0
    )
      return blocked();
    if (now - budget.at >= RELOAD_WINDOW_MS) budget = { at: now, count: 0 };
    if (budget.count >= MAX_WINDOW_RELOADS) return blocked();
    browser.sessionStorage.setItem(
      BUDGET_KEY,
      JSON.stringify({ at: budget.at, count: budget.count + 1 }),
    );
    browser.sessionStorage.setItem(RELOAD_KEY, build);
  } catch {
    return blocked();
  }
  browser.location.reload();
  return true;
}
