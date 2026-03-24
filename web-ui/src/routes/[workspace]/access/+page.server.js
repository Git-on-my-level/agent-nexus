import { normalizeWorkspaceSlug } from "$lib/workspacePaths";
import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver";

export async function load(event) {
  const workspaceSlug = normalizeWorkspaceSlug(event.params.workspace);
  const resolved = await resolveWorkspaceBySlug({
    event,
    workspaceSlug,
  });
  return { coreBaseUrl: resolved.workspace?.coreBaseUrl ?? "" };
}
