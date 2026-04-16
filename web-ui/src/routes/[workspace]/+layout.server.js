import { error } from "@sveltejs/kit";

import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver";

export async function load(event) {
  const resolved = await resolveWorkspaceBySlug({
    event,
    workspaceSlug: event.params.workspace,
  });

  if (resolved.error) {
    throw error(
      resolved.error.status,
      resolved.error.payload?.error?.message ||
        `Workspace '${event.params.workspace}' is unavailable.`,
    );
  }

  const coreBaseUrl = String(resolved.workspace.coreBaseUrl ?? "").trim();

  return {
    ...toPublicWorkspaceCatalog(resolved.catalog),
    workspace: {
      slug: resolved.workspace.slug,
      label: resolved.workspace.label,
      description: resolved.workspace.description,
      coreBaseUrl,
    },
  };
}
