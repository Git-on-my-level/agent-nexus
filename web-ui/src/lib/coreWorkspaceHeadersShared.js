import { WORKSPACE_HEADER_CONSTANTS } from "$lib/compat/workspaceCompat";
import { WORKSPACE_HEADER } from "$lib/workspacePaths";

/**
 * Pure mapping from org + workspace slugs to core routing headers.
 * Used by client and server header builders; keep in sync.
 *
 * @param {{ organizationSlug?: string | null, workspaceSlug?: string | null }} params
 * @returns {Record<string, string>}
 */
export function buildCoreWorkspaceRoutingHeadersFromSlugs({
  organizationSlug,
  workspaceSlug,
} = {}) {
  const org = String(organizationSlug ?? "").trim();
  const workspace = String(workspaceSlug ?? "").trim();
  const headers = {};
  if (workspace) {
    headers[WORKSPACE_HEADER] = workspace;
  }
  if (org) {
    headers[WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER] = org;
  }
  return headers;
}
