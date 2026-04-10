import { redirect } from "@sveltejs/kit";

import { resolveLegacyThreadCanonicalAppPath } from "$lib/server/threadTopicRouteRedirect";
import { workspacePath } from "$lib/workspacePaths";

export async function load(event) {
  const canonical = await resolveLegacyThreadCanonicalAppPath({
    fetchFn: event.fetch,
    workspaceSlug: event.params.workspace,
    legacyThreadId: event.params.threadId,
  });
  if (canonical) {
    throw redirect(
      307,
      workspacePath(event.params.workspace, `${canonical}${event.url.search}`),
    );
  }
  return {};
}
