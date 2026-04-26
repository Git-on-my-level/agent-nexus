import { buildCoreWorkspaceRoutingHeadersFromSlugs } from "$lib/coreWorkspaceHeadersShared";

/**
 * Workspace routing headers for server-side core fetches (mirrors
 * `buildCoreRequestContextHeaders` in `$lib/coreClientRequestHeaders.js`, but
 * takes explicit slugs for server-only callers).
 *
 * @param {{ organizationSlug?: string | null, workspaceSlug?: string | null }} params
 * @returns {Record<string, string>}
 */
export function buildServerCoreWorkspaceContextHeaders(params = {}) {
  return buildCoreWorkspaceRoutingHeadersFromSlugs(params);
}
