import { dev } from "$app/environment";
import { env as privateEnv } from "$env/dynamic/private";
import { AuthErrorCode } from "$lib/authErrorCodes.js";
import { normalizeBaseUrl } from "$lib/config.js";
import { CURRENT_VERSION } from "$lib/generated/version";
import { parseWorkspaceRouteSlugs } from "$lib/workspacePaths";
import {
  clearWorkspaceAuthSession,
  ensureWorkspaceAccessTokenForCoreProxy,
  getWorkspaceAuthSession,
  isRetryableWorkspaceRefreshFailure,
  readWorkspaceRefreshToken,
  refreshWorkspaceAuthSession,
  shouldClearWorkspaceAuthSessionAfterRetryableFailure,
} from "$lib/server/authSession";
import { coreBaseUrlForNodeFetch } from "$lib/server/coreBaseUrlForNodeFetch.js";
import { buildProxyRequestInit } from "$lib/server/coreProxy";
import { logServerEvent } from "$lib/server/devLog";
import { hostedWorkspaceCoreProxyHeaders } from "$lib/server/hostedWorkspaceCore";
import { classifyWorkspaceProxyPathShape } from "$lib/server/hostedWorkspaceProxy.js";
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver.js";

/**
 * In local hosted dev the browser only talks to web-ui; production Caddy routes
 * `/ws/{org}/{workspace}/stream/*` directly to anx-core. Mirror that here when
 * `dev` is true and the control plane is configured (see serve-control-plane.sh).
 *
 * @param {Record<string, string | undefined>} [env]
 * @returns {boolean}
 */
export function shouldProxyHostedWorkspaceStreamInDev(env = privateEnv) {
  if (!dev) {
    return false;
  }
  const override = String(env.ANX_HOSTED_DEV_STREAM_BYPASS ?? "")
    .trim()
    .toLowerCase();
  if (override === "0" || override === "false" || override === "off") {
    return false;
  }
  return Boolean(normalizeBaseUrl(env.ANX_CONTROL_BASE_URL));
}

/**
 * Strip `/ws/{organization}/{workspace}` from a hosted workspace proxy path.
 *
 * @param {string} pathname Stripped app path
 * @returns {string} Upstream path (e.g. `/stream/events`) or empty when invalid
 */
export function extractHostedWorkspaceUpstreamPath(pathname) {
  const { organizationSlug, workspaceSlug } =
    parseWorkspaceRouteSlugs(pathname);
  if (!organizationSlug || !workspaceSlug) {
    return "";
  }
  const prefix = `/ws/${organizationSlug}/${workspaceSlug}`;
  const normalized = String(pathname ?? "").trim();
  if (!normalized.startsWith(prefix)) {
    return "";
  }
  const rest = normalized.slice(prefix.length);
  if (!rest || rest === "/") {
    return "/";
  }
  return rest.startsWith("/") ? rest : `/${rest}`;
}

/**
 * @param {string} pathname Stripped app path
 * @returns {boolean}
 */
export function isHostedWorkspaceStreamPath(pathname) {
  const upstream = extractHostedWorkspaceUpstreamPath(pathname);
  return upstream === "/stream" || upstream.startsWith("/stream/");
}

function jsonErrorResponse(status, code, message) {
  return new Response(
    JSON.stringify({
      error: { code, message },
    }),
    {
      status,
      headers: {
        "content-type": "application/json",
        "X-ANX-UI-Version": CURRENT_VERSION,
      },
    },
  );
}

async function applyWorkspaceBearerAuth(
  event,
  organizationSlug,
  workspaceSlug,
  hostedCoreBaseUrl,
  requestInit,
) {
  const session = getWorkspaceAuthSession(
    event,
    organizationSlug,
    workspaceSlug,
  );
  const incomingAuth = event.request.headers.get("authorization");
  const accessTok = String(session?.accessToken ?? "").trim();
  const refreshTok = String(session?.refreshToken ?? "").trim();
  if (accessTok) {
    requestInit.headers.set("authorization", `Bearer ${accessTok}`);
  } else if (incomingAuth) {
    requestInit.headers.set("authorization", incomingAuth);
  } else if (refreshTok) {
    await ensureWorkspaceAccessTokenForCoreProxy({
      event,
      organizationSlug,
      workspaceSlug,
      coreBaseUrl: hostedCoreBaseUrl,
      session: { accessToken: "", refreshToken: refreshTok },
      headers: hostedWorkspaceCoreProxyHeaders(event),
    });
    const afterEnsure = getWorkspaceAuthSession(
      event,
      organizationSlug,
      workspaceSlug,
    );
    const nextAccess = String(afterEnsure?.accessToken ?? "").trim();
    if (nextAccess) {
      requestInit.headers.set("authorization", `Bearer ${nextAccess}`);
    }
  }
  return requestInit.headers.has("authorization");
}

async function refreshAndRetryCoreStream(
  event,
  organizationSlug,
  workspaceSlug,
  hostedCoreBaseUrl,
  targetUrl,
  hadAccessToken,
) {
  if (!readWorkspaceRefreshToken(event, organizationSlug, workspaceSlug)) {
    return null;
  }

  try {
    await refreshWorkspaceAuthSession({
      event,
      organizationSlug,
      workspaceSlug,
      coreBaseUrl: hostedCoreBaseUrl,
      headers: hostedWorkspaceCoreProxyHeaders(event),
    });
  } catch (error) {
    if (
      isRetryableWorkspaceRefreshFailure(error, {
        hadAccessToken,
        hadRefreshToken: true,
      })
    ) {
      if (
        shouldClearWorkspaceAuthSessionAfterRetryableFailure(
          event,
          organizationSlug,
          workspaceSlug,
        )
      ) {
        clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
      }
      return null;
    }
    clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
    return null;
  }

  const refreshedSession = getWorkspaceAuthSession(
    event,
    organizationSlug,
    workspaceSlug,
  );
  if (!refreshedSession?.accessToken) {
    return null;
  }

  const requestInit = buildProxyRequestInit(event);
  for (const [name, value] of Object.entries(
    hostedWorkspaceCoreProxyHeaders(event),
  )) {
    requestInit.headers.set(name, value);
  }
  requestInit.headers.set(
    "authorization",
    `Bearer ${refreshedSession.accessToken}`,
  );

  try {
    return await fetch(targetUrl, requestInit);
  } catch {
    return null;
  }
}

/**
 * Dev-only: proxy `/ws/{org}/{workspace}/stream/*` to anx-core on loopback,
 * matching production edge `uri strip_prefix` + `reverse_proxy` behavior.
 *
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {string} pathname Stripped app path starting with `/ws/`
 */
export async function proxyHostedWorkspaceStreamToCore(event, pathname) {
  if (!shouldProxyHostedWorkspaceStreamInDev()) {
    return jsonErrorResponse(
      404,
      "stream_bypasses_control_plane",
      "stream paths must be routed by edge directly to workspace runtime",
    );
  }

  const { organizationSlug, workspaceSlug } =
    parseWorkspaceRouteSlugs(pathname);
  if (!organizationSlug || !workspaceSlug) {
    const method = event.request.method.toUpperCase();
    logServerEvent("workspace.proxy.invalid_path", {
      code: AuthErrorCode.INVALID_WORKSPACE_PROXY_PATH,
      path_shape: classifyWorkspaceProxyPathShape(pathname),
      method,
      stream_bypass: true,
    });
    return jsonErrorResponse(
      400,
      AuthErrorCode.INVALID_WORKSPACE_PROXY_PATH,
      "Workspace proxy path must be /ws/{organization}/{workspace}/... .",
    );
  }

  const upstreamPath = extractHostedWorkspaceUpstreamPath(pathname);
  if (!isHostedWorkspaceStreamPath(pathname)) {
    return jsonErrorResponse(
      404,
      "stream_bypasses_control_plane",
      "stream paths must be routed by edge directly to workspace runtime",
    );
  }

  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug,
    workspaceSlug,
  });
  if (resolved.error) {
    return new Response(JSON.stringify(resolved.error.payload), {
      status: resolved.error.status,
      headers: {
        "content-type": "application/json",
        "X-ANX-UI-Version": CURRENT_VERSION,
      },
    });
  }

  const listenPort = Number.parseInt(
    String(resolved.workspace?.listenPort ?? ""),
    10,
  );
  if (!Number.isFinite(listenPort) || listenPort <= 0) {
    return jsonErrorResponse(
      503,
      "workspace_not_ready",
      `Workspace '${organizationSlug}/${workspaceSlug}' has no listen port yet. Please retry in a few seconds.`,
    );
  }

  const coreBase = coreBaseUrlForNodeFetch(`http://127.0.0.1:${listenPort}`);
  const targetUrl = new URL(upstreamPath, `${coreBase}/`);
  targetUrl.search = event.url.search;

  const hostedCoreBaseUrl = normalizeBaseUrl(resolved.coreBaseUrl);
  const requestInit = buildProxyRequestInit(event);
  for (const [name, value] of Object.entries(
    hostedWorkspaceCoreProxyHeaders(event),
  )) {
    requestInit.headers.set(name, value);
  }

  const hadAccessToken = await applyWorkspaceBearerAuth(
    event,
    organizationSlug,
    workspaceSlug,
    hostedCoreBaseUrl,
    requestInit,
  );

  let upstreamResponse;
  try {
    upstreamResponse = await fetch(targetUrl.toString(), requestInit);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    return jsonErrorResponse(
      503,
      "core_unreachable",
      `Unable to reach anx-core stream at ${coreBase}. ${reason}`,
    );
  }

  if (upstreamResponse.status === 401) {
    const retriedResponse = await refreshAndRetryCoreStream(
      event,
      organizationSlug,
      workspaceSlug,
      hostedCoreBaseUrl,
      targetUrl.toString(),
      hadAccessToken,
    );
    if (retriedResponse) {
      upstreamResponse = retriedResponse;
      if (upstreamResponse.status === 401) {
        clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
      }
    }
  }

  const responseHeaders = new Headers(upstreamResponse.headers);
  responseHeaders.delete("content-encoding");
  responseHeaders.delete("content-length");
  responseHeaders.set("X-ANX-UI-Version", CURRENT_VERSION);

  return new Response(upstreamResponse.body, {
    status: upstreamResponse.status,
    statusText: upstreamResponse.statusText,
    headers: responseHeaders,
  });
}
