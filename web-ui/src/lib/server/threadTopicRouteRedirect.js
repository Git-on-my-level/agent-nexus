import { createOarCoreClient } from "$lib/anxCoreClient";
import { topicRouteSegmentFromBackingThread } from "$lib/topicRouteUtils";
import { WORKSPACE_HEADER } from "$lib/workspacePaths";

/**
 * When `/threads/:id` refers to a backing thread with `topic_ref: topic:…`,
 * returns the canonical app path `/topics/:topicId` (no workspace prefix).
 * Otherwise returns null so the thread detail route can render (backing-only threads).
 */
export async function resolveLegacyThreadCanonicalAppPath({
  fetchFn,
  workspaceSlug,
  legacyThreadId,
}) {
  const raw = String(legacyThreadId ?? "").trim();
  if (!raw) return null;

  const slug = String(workspaceSlug ?? "").trim();
  const client = createOarCoreClient({
    fetchFn,
    requestContextHeadersProvider: () =>
      slug ? { [WORKSPACE_HEADER]: slug } : {},
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
