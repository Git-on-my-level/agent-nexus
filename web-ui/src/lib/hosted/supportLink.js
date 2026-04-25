/**
 * Hosted support contact resolution for error surfaces and footer links.
 * Set `PUBLIC_ANX_SUPPORT_URL` in the environment (https page or mailto:).
 */

/** @type {string} */
export const DEFAULT_HOSTED_SUPPORT_HREF = "mailto:support@agentnexus.com";

/**
 * @param {string} [configured] - Typically `PUBLIC_ANX_SUPPORT_URL` from SvelteKit public env.
 * @returns {string}
 */
export function resolveHostedSupportUrl(configured) {
  const u = String(configured ?? "").trim();
  if (u) return u;
  return DEFAULT_HOSTED_SUPPORT_HREF;
}

/**
 * Open in a new tab only for http(s) URLs.
 * @param {string} href
 */
export function supportLinkOpensInNewTab(href) {
  return /^https?:\/\//i.test(String(href ?? "").trim());
}
