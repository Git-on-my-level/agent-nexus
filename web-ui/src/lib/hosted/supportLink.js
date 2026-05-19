/**
 * Hosted support contact resolution for error surfaces and footer links.
 * Set `PUBLIC_ANX_SUPPORT_URL` in the environment (https page or mailto:).
 */

/** @type {string} */
export const DEFAULT_HOSTED_SUPPORT_HREF = "mailto:david@scalingforever.com";

/**
 * @param {string} [configured] - Typically `PUBLIC_ANX_SUPPORT_URL` from SvelteKit public env.
 * @returns {string}
 */
export function resolveHostedSupportUrl(configured) {
  const u = String(configured ?? "").trim();
  if (!u) return DEFAULT_HOSTED_SUPPORT_HREF;
  try {
    const parsed = new URL(u);
    if (parsed.protocol === "https:" || parsed.protocol === "mailto:") {
      return parsed.toString();
    }
  } catch {
    // Fall through to the stable support contact.
  }
  return DEFAULT_HOSTED_SUPPORT_HREF;
}

/**
 * Open in a new tab only for https URLs.
 * @param {string} href
 */
export function supportLinkOpensInNewTab(href) {
  try {
    return new URL(String(href ?? "").trim()).protocol === "https:";
  } catch {
    return false;
  }
}
