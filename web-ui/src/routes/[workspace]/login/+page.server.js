import { redirect } from "@sveltejs/kit";

import { loadWorkspaceAuthenticatedAgent } from "$lib/server/authSession";
import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver";
import { workspacePath, normalizeWorkspaceSlug } from "$lib/workspacePaths";

export async function load(event) {
  const workspaceSlug = normalizeWorkspaceSlug(event.params.workspace);
  const resolved = await resolveWorkspaceBySlug({
    event,
    workspaceSlug,
  });
  const workspace = resolved.workspace;

  if (!workspace || resolved.error) {
    return;
  }

  let agent;
  try {
    agent = await loadWorkspaceAuthenticatedAgent({
      event,
      workspaceSlug: resolved.workspaceSlug,
      coreBaseUrl: workspace.coreBaseUrl,
    });
  } catch (error) {
    if (error?.status) {
      return;
    }
    throw error;
  }

  if (agent?.agent_id) {
    throw redirect(307, workspacePath(resolved.workspaceSlug));
  }
}
