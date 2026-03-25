import { redirect } from "@sveltejs/kit";

import { workspacePath } from "$lib/workspacePaths";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";

export async function redirectToDefaultWorkspace(event, pathname = "") {
  const catalog = await resolveWorkspaceCatalog(event);
  throw redirect(307, workspacePath(catalog.defaultWorkspace.slug, pathname));
}
