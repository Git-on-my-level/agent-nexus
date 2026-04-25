import { createAnxCoreClient } from "$lib/anxCoreClient";
import { WORKSPACE_HEADER_CONSTANTS } from "$lib/compat/workspaceCompat";
import { topicRouteSegmentFromBackingThread } from "$lib/topicRouteUtils";
import { WORKSPACE_HEADER } from "$lib/workspacePaths";

/**
 * When `/threads/:id` refers to a backing thread with `topic_ref: topic:…`,
 * returns the canonical app path `/topics/:topicId` (no workspace prefix).
 * Otherwise returns null so the thread detail route can render (backing-only threads).
 */
export async function resolveLegacyThreadCanonicalAppPath({
  fetchFn,
  organizationSlug,
  workspaceSlug,
  legacyThreadId,
}) {
  const raw = String(legacyThreadId ?? "").trim();
  if (!raw) return null;

  const slug = String(workspaceSlug ?? "").trim();
  const org = String(organizationSlug ?? "").trim();
  const client = createAnxCoreClient({
    fetchFn,
    requestContextHeadersProvider: () => {
      const headers = {};
      if (slug) {
        headers[WORKSPACE_HEADER] = slug;
      }
      if (org) {
        headers[WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER] = org;
      }
      return headers;
    },
  });

  try {
    const res = await client.getThread(raw);
    const segment = topicRouteSegmentFromBackingThread(res?.thread ?? null);
    if (segment) {
      return `/topics/${encodeURIComponent(segment)}`;
    }
  } catch {
    // Thread missing or proxy error — keep serving the legacy thread URL.
  }

  return null;
}
