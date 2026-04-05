import { redirect } from "@sveltejs/kit";

import { resolveTopicRouteSegmentForLegacyThreadUrl } from "$lib/server/threadTopicRouteRedirect";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";
import { workspacePath } from "$lib/workspacePaths";

export async function load(event) {
  const catalog = await resolveWorkspaceCatalog(event);
  const slug = catalog.defaultWorkspace.slug;
  const segment = await resolveTopicRouteSegmentForLegacyThreadUrl({
    fetchFn: event.fetch,
    workspaceSlug: slug,
    legacyThreadId: event.params.threadId,
  });
  throw redirect(
    307,
    workspacePath(
      slug,
      `/topics/${encodeURIComponent(segment)}${event.url.search}`,
    ),
  );
}
