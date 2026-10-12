const RELOAD_KEY = "anx.chunk-recovery.v1";
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

/** At most one automatic reload per tab; blocked storage fails closed. */
export function recoverStaleChunk(error, browser = globalThis.window) {
  if (!browser || attempted || !isStaleChunkError(error)) return false;
  attempted = true;
  if (/(?:^|\/)overview\/?$/.test(browser.location.pathname || ""))
    warnOverviewRecovery("chunk-load", null, error);
  try {
    if (browser.sessionStorage.getItem(RELOAD_KEY)) return false;
    browser.sessionStorage.setItem(RELOAD_KEY, "1");
  } catch {
    return false;
  }
  browser.location.reload();
  return true;
}
