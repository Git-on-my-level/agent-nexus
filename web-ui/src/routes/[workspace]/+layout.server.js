import { error } from "@sveltejs/kit";

import { normalizeWorkspaceSlug } from "$lib/workspacePaths";
import { loadWorkspaceCatalog } from "$lib/server/workspaceCatalog";

export function load({ params }) {
  const catalog = loadWorkspaceCatalog();
  const workspaceSlug = normalizeWorkspaceSlug(params.workspace);
  const workspace = catalog.workspaceBySlug.get(workspaceSlug);

  if (!workspace) {
    throw error(404, `Workspace '${params.workspace}' is not configured.`);
  }

  return {
    workspace: {
      slug: workspace.slug,
      label: workspace.label,
      description: workspace.description,
    },
  };
}
