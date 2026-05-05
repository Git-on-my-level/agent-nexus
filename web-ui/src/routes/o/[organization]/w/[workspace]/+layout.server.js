import { dev } from "$app/environment";
import { env as privateEnv } from "$env/dynamic/private";
import { error, redirect } from "@sveltejs/kit";

import {
  createAnxCoreClient,
  verifyCoreSchemaVersion,
} from "$lib/anxCoreClient";
import { WORKSPACE_HEADER_CONSTANTS } from "$lib/compat/workspaceCompat";
import { sanitizeHostedReturnPath } from "$lib/hosted/launchFlow.js";
import {
  getAuthAccessCookieName,
  getAuthSessionCookieName,
} from "$lib/server/authSession.js";
import { logServerEvent } from "$lib/server/devLog";
import {
  hostedWorkspaceCoreBaseUrl,
  hostedWorkspaceCoreProxyHeaders,
} from "$lib/server/hostedWorkspaceCore.js";
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
import {
  WORKSPACE_HEADER,
  stripWorkspacePath,
  workspaceCompositeKey,
} from "$lib/workspacePaths";

/** Deduplicate handshake checks per workspace in this server process. */
const schemaCheckPromises = new Map();

/**
 * In dev, a hosted workspace often resolves to a same-origin `/ws/...` core URL
 * that is proxied to the control plane. If the workspace runtime (anx-core) is
 * not running yet, the proxy returns 5xx. Hard-failing SSR makes the app
 * unusable; we warn instead so the operator can start the stack
 * (e.g. `make serve` in `controlplane/`).
 * Also: when the UI's embedded command registry is newer than the running core's
 * `/meta/handshake` digest, degrade to a visible warning and rebuild/restart core.
 */
function shouldDegradeCoreSchemaCheckInDev(error) {
  if (!dev) {
    return false;
  }
  if (String(privateEnv.ANX_UI_SKIP_CORE_SCHEMA_CHECK ?? "").trim() === "1") {
    return true;
  }
  if (!(error instanceof Error)) {
    return false;
  }
  const st =
    typeof error.coreHttpStatus === "number"
      ? error.coreHttpStatus
      : error.cause && typeof error.cause.status === "number"
        ? error.cause.status
        : undefined;
  if (typeof st === "number" && st >= 502 && st <= 504) {
    return true;
  }
  const text = [error.message, error.cause && error.cause.message]
    .filter(Boolean)
    .join(" ");
  if (text.includes("Unable to reach control plane at")) {
    return true;
  }
  if (text.includes("workspace runtime backend is unavailable")) {
    return true;
  }
  if (/ECONNREFUSED|network\s*error|fetch failed/i.test(text)) {
    return true;
  }
  // UI and core built from different contract revisions: core is older (or stale binary).
  // In dev, warn on the page instead of hard-failing SSR; rebuild/restart anx-core to clear.
  if (text.includes("anx-core contract mismatch")) {
    return true;
  }
  return false;
}

function isSecureCookieRequest(event) {
  return event.url.protocol === "https:";
}

function workspaceHasCoreSession(event, organizationSlug, workspaceSlug) {
  const refreshToken = String(
    event.cookies.get(
      getAuthSessionCookieName(organizationSlug, workspaceSlug),
    ) ?? "",
  ).trim();
  const accessToken = String(
    event.cookies.get(
      getAuthAccessCookieName(organizationSlug, workspaceSlug),
    ) ?? "",
  ).trim();
  return refreshToken !== "" || accessToken !== "";
}

function workspaceRelativeReturnPath(event, organizationSlug, workspaceSlug) {
  const appPath = stripWorkspacePath(
    event.url.pathname,
    organizationSlug,
    workspaceSlug,
  );
  if (appPath === "/login") {
    return sanitizeHostedReturnPath(
      event.url.searchParams.get("return_to") ??
        event.url.searchParams.get("return_path") ??
        "/",
      "/",
    );
  }
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
        organizationSlug: event.params.organization,
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
    !workspaceHasCoreSession(
      event,
      resolved.workspace.organizationSlug,
      resolved.workspace.slug,
    )
  ) {
    const instruction = await provider.beginLaunchSession({
      event,
      workspaceId,
      organizationSlug: resolved.workspace.organizationSlug,
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

  const workOrg = resolved.workspace.organizationSlug;
  const workSlug = resolved.workspace.slug;
  const coreBaseUrl = String(resolved.workspace.coreBaseUrl ?? "").trim();
  const schemaCoreBaseUrl =
    provider.mode === "hosted"
      ? hostedWorkspaceCoreBaseUrl({
          organizationSlug: workOrg,
          workspaceSlug: workSlug,
        })
      : coreBaseUrl;

  let coreSchemaCheckWarning = "";

  if (
    workSlug &&
    schemaCoreBaseUrl &&
    event.url.searchParams.get("qa") !== "1"
  ) {
    const cacheKey = workspaceCompositeKey(workOrg, workSlug);
    if (!schemaCheckPromises.has(cacheKey)) {
      const client = createAnxCoreClient({
        baseUrl: schemaCoreBaseUrl,
        fetchFn: event.fetch,
        requestContextHeadersProvider: () => ({
          [WORKSPACE_HEADER]: workSlug,
          [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: workOrg,
          ...(provider.mode === "hosted"
            ? hostedWorkspaceCoreProxyHeaders(event)
            : {}),
        }),
      });
      const promise = verifyCoreSchemaVersion(client)
        .then(() => "")
        .catch((error) => {
          schemaCheckPromises.delete(cacheKey);
          if (shouldDegradeCoreSchemaCheckInDev(error)) {
            logServerEvent("workspace.layout.schema_check_degraded", {
              org: workOrg,
              slug: workSlug,
            });
            return error instanceof Error ? error.message : String(error);
          }
          throw error;
        });
      schemaCheckPromises.set(cacheKey, promise);
    }
    coreSchemaCheckWarning = await schemaCheckPromises.get(cacheKey);
  }

  return {
    ...toPublicWorkspaceCatalog(catalog),
    workspace: {
      organizationSlug: workOrg,
      slug: workSlug,
      label: resolved.workspace.label,
      description: resolved.workspace.description,
      coreBaseUrl,
      workspaceId,
    },
    ...(coreSchemaCheckWarning ? { coreSchemaCheckWarning } : {}),
  };
}
