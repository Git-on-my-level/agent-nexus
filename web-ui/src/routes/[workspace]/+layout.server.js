import { env } from "$env/dynamic/private";
import { error, redirect } from "@sveltejs/kit";

import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver";
import { DEFAULT_WORKSPACE_SLUG } from "$lib/workspacePaths";

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
  const saasPackedHostDev =
    env.OAR_SAAS_PACKED_HOST_DEV === "true" ||
    env.OAR_SAAS_PACKED_HOST_DEV === "1";
  if (
    saasPackedHostDev &&
    resolved.workspace.slug === DEFAULT_WORKSPACE_SLUG &&
    !coreBaseUrl
  ) {
    throw redirect(307, "/auth");
  }

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
