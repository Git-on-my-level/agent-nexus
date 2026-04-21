import { env as privateEnv } from "$env/dynamic/private";
import { error, redirect } from "@sveltejs/kit";

import {
  buildHostedSignInPath,
  sanitizeHostedReturnPath,
} from "$lib/hosted/launchFlow.js";
import { isHostedWebUiShell } from "$lib/server/controlPlaneWorkspace";
import { logServerEvent } from "$lib/server/devLog";
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

/**
 * In hosted dev/SaaS mode the workspace catalog is owned by the control plane.
 * If we can't resolve a workspace because the SSR request has no CP access
 * token (cookie + env both empty), the user is simply unauthenticated — bounce
 * them to /hosted/signin instead of returning the misleading
 * "workspace not configured" 404 page.
 */
function shouldRedirectToHostedSignIn(event) {
  if (!isHostedWebUiShell(privateEnv)) {
    return false;
  }
  const envToken = String(
    privateEnv.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN ?? "",
  ).trim();
  if (envToken) {
    return false;
  }
  const cookieToken = String(
    event.cookies.get("oar_cp_dev_access_token") ?? "",
  ).trim();
  return cookieToken === "";
}

export async function load(event) {
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: event.params.organization,
    workspaceSlug: event.params.workspace,
  });

  if (resolved.error) {
    const code = resolved.error.payload?.error?.code ?? "workspace_unavailable";
    const message =
      resolved.error.payload?.error?.message ||
      `Workspace '${event.params.organization}/${event.params.workspace}' is unavailable.`;
    logServerEvent(
      "workspace.layout.resolve_failed",
      {
        org: event.params.organization,
        slug: event.params.workspace,
        status: resolved.error.status,
        code,
        message,
      },
      { level: "warn" },
    );

    // Only bounce to /hosted/signin when the workspace appears truly unknown
    // and the SSR request has no CP token. A "workspace_not_ready" 503 means
    // we did resolve the workspace from the CP but its core hasn't started
    // yet — bouncing to signin would loop the user.
    if (code !== "workspace_not_ready" && shouldRedirectToHostedSignIn(event)) {
      const returnPath = sanitizeHostedReturnPath(
        `${event.url.pathname}${event.url.search}`,
      );
      const target = buildHostedSignInPath({
        workspaceSlug: event.params.workspace,
        returnPath,
      });
      logServerEvent("workspace.layout.redirect_to_signin", {
        org: event.params.organization,
        slug: event.params.workspace,
        target,
      });
      throw redirect(307, target);
    }

    // Pass the structured code through so $page.error.code is available
    // for +error.svelte to render a useful, actionable message instead of
    // the generic "Internal Error" fallback.
    throw error(resolved.error.status, { message, code });
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
