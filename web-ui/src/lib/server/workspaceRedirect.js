import { env } from "$env/dynamic/private";
import { redirect } from "@sveltejs/kit";

import { resolveWorkspaceEnv } from "$lib/compat/workspaceCompat";
import { workspacePath } from "$lib/workspacePaths";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";

export async function redirectToDefaultWorkspace(event, pathname = "") {
  const saasPackedHostDev =
    env.OAR_SAAS_PACKED_HOST_DEV === "true" ||
    env.OAR_SAAS_PACKED_HOST_DEV === "1";
  const { OAR_WORKSPACES } = resolveWorkspaceEnv(env);
  if (saasPackedHostDev && !String(OAR_WORKSPACES ?? "").trim()) {
    throw redirect(307, "/auth");
  }

  const catalog = await resolveWorkspaceCatalog(event);
  throw redirect(307, workspacePath(catalog.defaultWorkspace.slug, pathname));
}
