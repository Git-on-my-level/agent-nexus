import { env as privateEnv } from "$env/dynamic/private";
import { error, redirect } from "@sveltejs/kit";

import { sanitizeHostedReturnPath } from "$lib/hosted/launchFlow.js";
import {
  getAuthAccessCookieName,
  getAuthSessionCookieName,
} from "$lib/server/authSession.js";
import { logServerEvent } from "$lib/server/devLog";
import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import { handleLaunchInstruction } from "$lib/server/outOfWorkspace/launchSession.js";
import {
  LAST_WORKSPACE_COOKIE,
  lastWorkspaceCookieValue,
} from "$lib/server/workspaceRedirect";
import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import {
  resolveWorkspaceCatalog,
  resolveWorkspaceInRoute,
} from "$lib/server/workspaceResolver";
import { stripWorkspacePath } from "$lib/workspacePaths";

function isSecureCookieRequest(event) {
  return event.url.protocol === "https:";
}

function workspaceHasCoreSession(event, workspaceSlug) {
  const refreshToken = String(
    event.cookies.get(getAuthSessionCookieName(workspaceSlug)) ?? "",
  ).trim();
  const accessToken = String(
    event.cookies.get(getAuthAccessCookieName(workspaceSlug)) ?? "",
  ).trim();
  return refreshToken !== "" || accessToken !== "";
}

function workspaceRelativeReturnPath(event, organizationSlug, workspaceSlug) {
  const appPath = stripWorkspacePath(
    event.url.pathname,
    organizationSlug,
    workspaceSlug,
  );
  return sanitizeHostedReturnPath(`${appPath}${event.url.search}`, "/");
}

export async function load(event) {
  const provider =
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv);
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

    if (
      code === "workspace_not_configured" &&
      provider.mode === "hosted" &&
      resolved.outOfWorkspaceUnauthenticated
    ) {
      const signInUrl = provider.buildSignInUrl({
        workspaceSlug: event.params.workspace,
        returnPath: workspaceRelativeReturnPath(
          event,
          event.params.organization,
          event.params.workspace,
        ),
      });
      if (signInUrl) {
        logServerEvent("workspace.layout.redirect_to_signin", {
          org: event.params.organization,
          slug: event.params.workspace,
          target: signInUrl,
        });
        throw redirect(307, signInUrl);
      }
    }

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

  const workspaceId = String(
    resolved.workspace.workspaceId ?? resolved.workspace.id ?? "",
  ).trim();
  if (
    provider.mode === "hosted" &&
    workspaceId &&
    !workspaceHasCoreSession(event, resolved.workspace.slug)
  ) {
    const instruction = await provider.beginLaunchSession({
      event,
      workspaceId,
      workspaceSlug: resolved.workspace.slug,
      returnPath: workspaceRelativeReturnPath(
        event,
        resolved.workspace.organizationSlug,
        resolved.workspace.slug,
      ),
    });
    handleLaunchInstruction(instruction);
  }

  const catalog = await resolveWorkspaceCatalog(event, {
    prefetchedResolved: resolved,
  });

  return {
    ...toPublicWorkspaceCatalog(catalog),
    workspace: {
      organizationSlug: resolved.workspace.organizationSlug,
      slug: resolved.workspace.slug,
      label: resolved.workspace.label,
      description: resolved.workspace.description,
      coreBaseUrl: String(resolved.workspace.coreBaseUrl ?? "").trim(),
    },
  };
}
