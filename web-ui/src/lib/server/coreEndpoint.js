/**
 * Join a core base URL (which may include a hosted `/ws/{org}/{workspace}` prefix)
 * with an API pathname. Using `new URL(path, base + "/")` preserves the full
 * path prefix; naive string concat or resolving from origin-only bases drops it.
 *
 * @param {string} coreBaseUrl
 * @param {string} pathname Absolute path beginning with `/` (e.g. `/auth/token`).
 * @returns {string}
 */
export function coreEndpointURL(coreBaseUrl, pathname) {
  const base = String(coreBaseUrl ?? "")
    .trim()
    .replace(/\/+$/, "");
  const path = String(pathname ?? "")
    .trim()
    .replace(/^\/+/, "");
  return new URL(path, `${base}/`).toString();
}
