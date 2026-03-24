import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";

export async function load(event) {
  return toPublicWorkspaceCatalog(await resolveWorkspaceCatalog(event));
}
