import { env as privateEnv } from "$env/dynamic/private";
import { AuthErrorCode } from "$lib/authErrorCodes.js";
import { normalizeBaseUrl } from "$lib/config.js";
import { CURRENT_VERSION } from "$lib/generated/version";
import { parseWorkspaceRouteSlugs } from "$lib/workspacePaths";
import {
  clearWorkspaceAuthSession,
  getWorkspaceAuthSession,
  isRetryableWorkspaceRefreshFailure,
  readWorkspaceRefreshToken,
  refreshWorkspaceAuthSession,
  shouldClearWorkspaceAuthSessionAfterRetryableFailure,
} from "$lib/server/authSession";
import { buildProxyRequestInit } from "$lib/server/coreProxy";
import { logServerEvent } from "$lib/server/devLog";

export function isHostedWorkspaceProxyPath(pathname) {
  return String(pathname ?? "").startsWith("/ws/");
}

async function refreshAndRetryHostedWorkspaceRequest(
  event,
  controlPlaneBaseUrl,
  organizationSlug,
  workspaceSlug,
  pathname,
  search,
  requestBody,
  hadAccessToken,
) {
  if (!readWorkspaceRefreshToken(event, organizationSlug, workspaceSlug)) {
    return null;
  }

  const hostedCoreBaseUrl = new URL(
    `/ws/${organizationSlug}/${workspaceSlug}`,
    `${controlPlaneBaseUrl}/`,
  ).toString();

  try {
    await refreshWorkspaceAuthSession({
      event,
      organizationSlug,
      workspaceSlug,
      coreBaseUrl: hostedCoreBaseUrl,
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

  const targetUrl = new URL(
    `${pathname}${search}`,
    `${controlPlaneBaseUrl}/`,
  ).toString();
  const requestInit = buildProxyRequestInit(event, {
    body: requestBody,
  });
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
 * Coarse classification for malformed `/ws/...` paths (safe to log; no raw path).
 * @param {string} pathname Stripped app path
 * @returns {"wrong_prefix"|"prefix_only"|"too_few_segments"|"empty_segment"|"unknown_invalid"}
 */
export function classifyWorkspaceProxyPathShape(pathname) {
  const p = String(pathname ?? "");
  if (!p.startsWith("/ws")) {
    return "wrong_prefix";
  }
  if (p === "/ws") {
    return "wrong_prefix";
  }
  const afterWs = p.slice(3);
  if (afterWs === "" || afterWs === "/") {
    return "prefix_only";
  }
  if (afterWs.startsWith("//")) {
    return "empty_segment";
  }
  if (!afterWs.startsWith("/")) {
    return "wrong_prefix";
  }
  const rest = afterWs.slice(1);
  if (rest.includes("//")) {
    return "empty_segment";
  }
  const segments = rest.split("/").filter((s) => s.length > 0);
  if (segments.length < 2) {
    return "too_few_segments";
  }
  return "unknown_invalid";
}

/**
 * Forward `/ws/{org}/{workspace}/...` to the control plane workspace proxy.
 * Preserves body, strips hop-by-hop-ish response headers, attaches `X-ANX-UI-Version`.
 *
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {string} pathname Stripped app path starting with `/ws/`
 */
export async function proxyToControlPlaneWorkspace(event, pathname) {
  const controlPlaneBaseUrl = normalizeBaseUrl(privateEnv.ANX_CONTROL_BASE_URL);
  if (!controlPlaneBaseUrl) {
    return new Response(
      JSON.stringify({
        error: {
          code: "control_plane_not_configured",
          message:
            "Workspace proxy path requires ANX_CONTROL_BASE_URL to be configured.",
        },
      }),
      {
        status: 503,
        headers: {
          "content-type": "application/json",
          "X-ANX-UI-Version": CURRENT_VERSION,
        },
      },
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
    });
    return new Response(
      JSON.stringify({
        error: {
          code: AuthErrorCode.INVALID_WORKSPACE_PROXY_PATH,
          message:
            "Workspace proxy path must be /ws/{organization}/{workspace}/... .",
        },
      }),
      {
        status: 400,
        headers: {
          "content-type": "application/json",
          "X-ANX-UI-Version": CURRENT_VERSION,
        },
      },
    );
  }

  const method = event.request.method.toUpperCase();
  let requestBody;
  if (method !== "GET" && method !== "HEAD") {
    const payload = new Uint8Array(await event.request.arrayBuffer());
    requestBody = payload.byteLength > 0 ? payload : undefined;
  }

  const targetUrl = new URL(
    `${pathname}${event.url.search}`,
    `${controlPlaneBaseUrl}/`,
  ).toString();
  const requestInit = buildProxyRequestInit(event, {
    body: requestBody,
  });
  let upstreamResponse;
  const session = getWorkspaceAuthSession(
    event,
    organizationSlug,
    workspaceSlug,
  );
  const incomingAuth = event.request.headers.get("authorization");
  if (session?.accessToken) {
    requestInit.headers.set("authorization", `Bearer ${session.accessToken}`);
  } else if (incomingAuth) {
    requestInit.headers.set("authorization", incomingAuth);
  } else if (session?.refreshToken) {
    const refreshedResponse = await refreshAndRetryHostedWorkspaceRequest(
      event,
      controlPlaneBaseUrl,
      organizationSlug,
      workspaceSlug,
      pathname,
      event.url.search,
      requestBody,
      false,
    );
    if (refreshedResponse) {
      upstreamResponse = refreshedResponse;
    }
  }

  try {
    upstreamResponse ??= await fetch(targetUrl, requestInit);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    return new Response(
      JSON.stringify({
        error: {
          code: "control_plane_unreachable",
          message: `Unable to reach control plane at ${controlPlaneBaseUrl}.`,
          reason,
        },
      }),
      {
        status: 503,
        headers: {
          "content-type": "application/json",
          "X-ANX-UI-Version": CURRENT_VERSION,
        },
      },
    );
  }

  if (upstreamResponse.status === 401) {
    const retriedResponse = await refreshAndRetryHostedWorkspaceRequest(
      event,
      controlPlaneBaseUrl,
      organizationSlug,
      workspaceSlug,
      pathname,
      event.url.search,
      requestBody,
      Boolean(session?.accessToken),
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
