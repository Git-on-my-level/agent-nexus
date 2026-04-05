import { createOarCoreClient } from "$lib/oarCoreClient";
import { topicRouteSegmentFromBackingThread } from "$lib/topicRouteUtils";
import { WORKSPACE_HEADER } from "$lib/workspacePaths";

/**
 * Resolves the `/topics/:id` segment for legacy `/threads/:threadId` URLs.
 * Uses `threads.inspect` when available; falls back to the raw param.
 */
export async function resolveTopicRouteSegmentForLegacyThreadUrl({
  fetchFn,
  workspaceSlug,
  legacyThreadId,
}) {
  const raw = String(legacyThreadId ?? "").trim();
  if (!raw) return raw;

  const slug = String(workspaceSlug ?? "").trim();
  const client = createOarCoreClient({
    fetchFn,
    requestContextHeadersProvider: () =>
      slug ? { [WORKSPACE_HEADER]: slug } : {},
  });

  try {
    const res = await client.getThread(raw);
    const segment = topicRouteSegmentFromBackingThread(res?.thread ?? null);
    if (segment) return segment;
  } catch {
    // Thread missing or proxy error — preserve legacy segment.
  }

  return raw;
}
