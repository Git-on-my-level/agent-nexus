import { redirect } from "@sveltejs/kit";

import { loadWorkspaceAuthenticatedAgent } from "$lib/server/authSession";
import { loadWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import { workspacePath, normalizeWorkspaceSlug } from "$lib/workspacePaths";

export async function load(event) {
  const catalog = loadWorkspaceCatalog();
  const workspaceSlug = normalizeWorkspaceSlug(event.params.workspace);
  const workspace = catalog.workspaceBySlug.get(workspaceSlug);

  if (!workspace) {
    return;
  }

  let agent;
  try {
    agent = await loadWorkspaceAuthenticatedAgent({
      event,
      workspaceSlug,
      coreBaseUrl: workspace.coreBaseUrl,
    });
  } catch (error) {
    if (error?.status) {
      return;
    }
    throw error;
  }

  if (agent?.agent_id) {
    throw redirect(307, workspacePath(workspaceSlug));
  }
}
