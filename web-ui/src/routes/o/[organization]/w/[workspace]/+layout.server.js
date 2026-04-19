import { error } from "@sveltejs/kit";

import {
  LAST_WORKSPACE_COOKIE,
  lastWorkspaceCookieValue,
} from "$lib/server/workspaceRedirect";
import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import {
  resolveWorkspaceCatalog,
  resolveWorkspaceInRoute,
} from "$lib/server/workspaceResolver";

function isSecureCookieRequest(event) {
  return event.url.protocol === "https:";
}

export async function load(event) {
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: event.params.organization,
    workspaceSlug: event.params.workspace,
  });

  if (resolved.error) {
    throw error(
      resolved.error.status,
      resolved.error.payload?.error?.message ||
        `Workspace '${event.params.organization}/${event.params.workspace}' is unavailable.`,
    );
  }

  event.cookies.set(
    LAST_WORKSPACE_COOKIE,
    lastWorkspaceCookieValue(
      resolved.workspace.organizationSlug,
      resolved.workspace.slug,
    ),
    {
      path: "/",
      httpOnly: true,
      sameSite: "lax",
      secure: isSecureCookieRequest(event),
      maxAge: 60 * 60 * 24 * 180,
    },
  );

  const coreBaseUrl = String(resolved.workspace.coreBaseUrl ?? "").trim();
  const catalog = await resolveWorkspaceCatalog(event);

  return {
    ...toPublicWorkspaceCatalog(catalog),
    workspace: {
      organizationSlug: resolved.workspace.organizationSlug,
      slug: resolved.workspace.slug,
      label: resolved.workspace.label,
      description: resolved.workspace.description,
      coreBaseUrl,
    },
  };
}
