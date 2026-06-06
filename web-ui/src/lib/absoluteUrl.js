import { browser } from "$app/environment";

/**
 * Resolve an app-relative path to an absolute, shareable URL. Falls back to
 * the relative path during SSR (no `window`).
 *
 * @param {string} path
 * @returns {string}
 */
export function absoluteUrl(path) {
  const p = String(path ?? "");
  if (!p || !browser) return p;
  try {
    return new URL(p, window.location.origin).toString();
  } catch {
    return p;
  }
}
