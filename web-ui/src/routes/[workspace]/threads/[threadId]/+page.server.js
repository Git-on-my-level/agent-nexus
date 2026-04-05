import { redirect } from "@sveltejs/kit";

import { resolveTopicRouteSegmentForLegacyThreadUrl } from "$lib/server/threadTopicRouteRedirect";
import { workspacePath } from "$lib/workspacePaths";

export async function load(event) {
  const segment = await resolveTopicRouteSegmentForLegacyThreadUrl({
    fetchFn: event.fetch,
    workspaceSlug: event.params.workspace,
    legacyThreadId: event.params.threadId,
  });
  throw redirect(
    307,
    workspacePath(
      event.params.workspace,
      `/topics/${encodeURIComponent(segment)}${event.url.search}`,
    ),
  );
}
