const RELOAD_KEY = "anx.chunk-recovery.v1";
let attempted = false;

export function isStaleChunkError(error) {
  return /Failed to fetch dynamically imported module|Importing a module script failed|Loading chunk .* failed|error loading dynamically imported module/i.test(
    error?.message || String(error || ""),
  );
}

/** At most one automatic reload per tab; blocked storage fails closed. */
export function recoverStaleChunk(error, browser = globalThis.window) {
  if (!browser || attempted || !isStaleChunkError(error)) return false;
  attempted = true;
  try {
    if (browser.sessionStorage.getItem(RELOAD_KEY)) return false;
    browser.sessionStorage.setItem(RELOAD_KEY, "1");
  } catch {
    return false;
  }
  browser.location.reload();
  return true;
}
