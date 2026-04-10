import { redirect } from "@sveltejs/kit";

import { resolveLegacyThreadCanonicalAppPath } from "$lib/server/threadTopicRouteRedirect";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";
import { workspacePath } from "$lib/workspacePaths";

export async function load(event) {
  const catalog = await resolveWorkspaceCatalog(event);
  const slug = catalog.defaultWorkspace.slug;
  const canonical = await resolveLegacyThreadCanonicalAppPath({
    fetchFn: event.fetch,
    workspaceSlug: slug,
    legacyThreadId: event.params.threadId,
  });
  if (canonical) {
    throw redirect(307, workspacePath(slug, `${canonical}${event.url.search}`));
  }
  return {};
}
