import { dev } from "$app/environment";
import { env as privateEnv } from "$env/dynamic/private";
import { error, redirect } from "@sveltejs/kit";

import {
  createAnxCoreClient,
  verifyCoreSchemaVersion,
} from "$lib/anxCoreClient";
import { WORKSPACE_HEADER_CONSTANTS } from "$lib/compat/workspaceCompat";
import { sanitizeReturnPath } from "$lib/workspaceLaunchFlow.js";
import { loadWorkspaceAuthenticatedAgent } from "$lib/server/authSession.js";
import { logServerEvent } from "$lib/server/devLog";
import {
  hostedWorkspaceCoreBaseUrl,
  hostedWorkspaceCoreProxyHeaders,
} from "$lib/server/hostedWorkspaceCore.js";
import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import {
  LAST_WORKSPACE_COOKIE,
  lastWorkspaceCookieValue,
} from "$lib/server/workspaceRedirect";
import {
  toPublicWorkspaceCatalog,
  workspaceSessionProbe,
} from "$lib/server/workspaceCatalog";
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

function workspaceRelativeReturnPath(event, organizationSlug, workspaceSlug) {
  const appPath = stripWorkspacePath(
    event.url.pathname,
    organizationSlug,
    workspaceSlug,
  );
  if (appPath === "/login") {
    return sanitizeReturnPath(
      event.url.searchParams.get("return_to") ??
        event.url.searchParams.get("return_path") ??
        "/",
      "/",
    );
  }
  return sanitizeReturnPath(`${appPath}${event.url.search}`, "/");
}

export async function load(event) {
  const provider =
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv);
  if (provider.mode === "hosted")
    event.setHeaders?.({ "cache-control": "private, no-store" });
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

  if (provider.mode !== "hosted")
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
        secure: !dev || isSecureCookieRequest(event),
        maxAge: 60 * 60 * 24 * 180,
      },
    );

  const workspaceId = String(
    resolved.workspace.workspaceId ?? resolved.workspace.id ?? "",
  ).trim();
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

  // Validate existing cookies while the compatibility check runs, not after hydration.
  const sessionPromise =
    provider.mode === "hosted"
      ? loadWorkspaceAuthenticatedAgent({
          readOnly: true,
          event,
          organizationSlug: workOrg,
          workspaceSlug: workSlug,
          coreBaseUrl: schemaCoreBaseUrl,
          headers: {
            ...hostedWorkspaceCoreProxyHeaders(event),
            purpose: "prefetch",
          },
        })
      : Promise.resolve(undefined);
  // Attach a rejection handler immediately while the schema check is pending.
  const sessionResult = sessionPromise.then(
    (agent) => ({ agent }),
    (failure) => ({ failure }),
  );
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
            ? { ...hostedWorkspaceCoreProxyHeaders(event), purpose: "prefetch" }
            : {}),
        }),
      });
      const promise = verifyCoreSchemaVersion(client)
        .then(() => "")
        .catch((error) => {
          schemaCheckPromises.delete(cacheKey);
          // Passive reads cannot wake a sleeping runtime. Activation checks the
          // schema after establishing the selected workspace's session.
          if (provider.mode === "hosted" && error?.coreHttpStatus === 503)
            return "";
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

  const session = await sessionResult;
  /*
   * On the viewer's own navigation an unreadable session is not an error page:
   * the shell paints its skeleton and the selection POST establishes it (or
   * reports the failure with its own Retry). Route-data loads keep failing
   * loudly, so a broken preload is still visible rather than silently empty.
   */
  if (session.failure && event.isDataRequest)
    throw error(
      session.failure.status || 503,
      "Could not validate workspace session.",
    );
  if (session.failure)
    logServerEvent(
      "workspace.layout.session_read_deferred",
      {
        org: workOrg,
        slug: workSlug,
        status: session.failure.status ?? 0,
      },
      { level: "warn" },
    );
  return {
    ...(provider.mode === "hosted"
      ? { workspaceSession: { agent: session.agent ?? null } }
      : {}),
    /*
     * `page.data` merges root-first, so this copy of `workspaces` replaces the
     * root layout's. It has to carry `hasSession` too, or the Overview reads a
     * catalog that says every workspace is readable and fans out to all of
     * them.
     */
    ...toPublicWorkspaceCatalog(catalog, {
      hasSession: workspaceSessionProbe(event, provider),
    }),
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
