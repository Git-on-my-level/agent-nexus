import { error } from "@sveltejs/kit";

import { normalizeWorkspaceSlug } from "$lib/workspacePaths";
import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver";

export async function load(event) {
  const workspaceSlug = normalizeWorkspaceSlug(event.params.workspace);
  const resolved = await resolveWorkspaceBySlug({
    event,
    workspaceSlug,
  });

  if (resolved.error) {
    throw error(
      resolved.error.status,
      resolved.error.payload?.error?.message ||
        `Workspace '${event.params.workspace}' is unavailable.`,
    );
  }

  return {
    ...toPublicWorkspaceCatalog(resolved.catalog),
    workspace: {
      slug: resolved.workspace.slug,
      label: resolved.workspace.label,
      description: resolved.workspace.description,
    },
  };
}
