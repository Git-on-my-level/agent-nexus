/**
 * Normalize a core base URL for Node `fetch()` (avoids localhost → ::1 vs 127.0.0.1 mismatch).
 * Use with `coreEndpointURL` from `coreEndpoint.js` for server-side fetches only.
 *
 * - Empty input → empty string
 * - Invalid URL string → trim trailing slashes
 * - `localhost` host → `127.0.0.1` (port and path prefix preserved)
 * - Idempotent for already-`127.0.0.1` bases
 *
 * @param {string} url
 * @returns {string}
 */
export function coreBaseUrlForNodeFetch(url) {
  const trimmed = String(url ?? "").trim();
  if (!trimmed) {
    return "";
  }
  try {
    const parsed = new URL(trimmed.endsWith("/") ? trimmed : `${trimmed}/`);
    if (parsed.hostname === "localhost") {
      parsed.hostname = "127.0.0.1";
    }
    let out = parsed.toString();
    if (out.endsWith("/")) {
      out = out.slice(0, -1);
    }
    return out;
  } catch {
    return trimmed.replace(/\/+$/, "");
  }
}
