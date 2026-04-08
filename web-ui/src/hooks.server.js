import { dev } from "$app/environment";
import { env as privateEnv } from "$env/dynamic/private";
import { isProxyableCommand } from "$lib/coreRouteCatalog";
import { getWorkspaceHeader } from "$lib/compat/workspaceCompat";
import { CURRENT_VERSION } from "$lib/generated/version";
import { stripBasePath } from "$lib/workspacePaths";
import {
  clearWorkspaceAuthSession,
  getWorkspaceAuthSession,
  isRetryableWorkspaceRefreshFailure,
  readWorkspaceRefreshToken,
  refreshWorkspaceAuthSession,
  shouldClearWorkspaceAuthSessionAfterRetryableFailure,
} from "$lib/server/authSession";
import { buildProxyRequestInit } from "$lib/server/coreProxy";
import { resolveProxyTarget } from "$lib/server/proxyWorkspaceTarget";
import { isDirectCoreProxyPath } from "$lib/server/directCoreProxyPaths";

function isDocumentNavigationRequest(request) {
  const method = request.method.toUpperCase();
  if (method !== "GET" && method !== "HEAD") {
    return false;
  }

  const secFetchDest = request.headers.get("sec-fetch-dest");
  if (secFetchDest === "document") {
    return true;
  }

  const accept = request.headers.get("accept") ?? "";
  return accept.includes("text/html");
}

function shouldBypassProxy(pathname, method) {
  const normalizedMethod = method.toUpperCase();
  return (
    normalizedMethod === "POST" &&
    (pathname === "/auth/passkey/login/verify" ||
      pathname === "/auth/passkey/register/verify")
  );
}

async function refreshAndRetry(
  event,
  coreBaseUrl,
  workspaceSlug,
  targetUrl,
  requestBody,
  hadAccessToken,
) {
  if (!readWorkspaceRefreshToken(event, workspaceSlug)) {
    return null;
  }

  try {
    await refreshWorkspaceAuthSession({
      event,
      workspaceSlug,
      coreBaseUrl,
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
          workspaceSlug,
        )
      ) {
        clearWorkspaceAuthSession(event, workspaceSlug);
      }
      return null;
    }

    clearWorkspaceAuthSession(event, workspaceSlug);
    return null;
  }

  const refreshedSession = getWorkspaceAuthSession(event, workspaceSlug);
  if (!refreshedSession?.accessToken) {
    return null;
  }

  const requestInit = buildProxyRequestInit(event, {
    body: requestBody,
  });
  requestInit.headers.delete("cookie");
  requestInit.headers.delete("authorization");
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

async function proxyToCore(event, coreBaseUrl, workspaceSlug) {
  const corePathname = stripBasePath(event.url.pathname);
  const targetUrl = new URL(
    `${corePathname}${event.url.search}`,
    `${coreBaseUrl}/`,
  ).toString();
  const method = event.request.method.toUpperCase();
  let requestBody;
  if (method !== "GET" && method !== "HEAD") {
    const payload = new Uint8Array(await event.request.arrayBuffer());
    requestBody = payload.byteLength > 0 ? payload : undefined;
  }

  const incomingAuth = event.request.headers.get("authorization");
  const requestInit = buildProxyRequestInit(event, {
    body: requestBody,
  });
  const session = getWorkspaceAuthSession(event, workspaceSlug);
  if (session?.accessToken) {
    requestInit.headers.set("authorization", `Bearer ${session.accessToken}`);
  } else if (incomingAuth) {
    requestInit.headers.set("authorization", incomingAuth);
  }

  let upstreamResponse;
  try {
    upstreamResponse = await fetch(targetUrl, requestInit);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    return new Response(
      JSON.stringify({
        error: {
          code: "core_unreachable",
          message: `Unable to reach oar-core at ${coreBaseUrl}. Start backend with ../core/scripts/dev and retry.`,
          reason,
        },
      }),
      {
        status: 503,
        headers: {
          "content-type": "application/json",
          "X-OAR-UI-Version": CURRENT_VERSION,
        },
      },
    );
  }

  if (upstreamResponse.status === 401) {
    const retriedResponse = await refreshAndRetry(
      event,
      coreBaseUrl,
      workspaceSlug,
      targetUrl,
      requestBody,
      Boolean(session?.accessToken),
    );
    if (retriedResponse) {
      upstreamResponse = retriedResponse;
      if (upstreamResponse.status === 401) {
        clearWorkspaceAuthSession(event, workspaceSlug);
      }
    }
  }

  const responseHeaders = new Headers(upstreamResponse.headers);
  responseHeaders.delete("content-encoding");
  responseHeaders.delete("content-length");

  return new Response(upstreamResponse.body, {
    status: upstreamResponse.status,
    statusText: upstreamResponse.statusText,
    headers: responseHeaders,
  });
}

const GOOGLE_FONTS_STYLE = "https://fonts.googleapis.com";
const GOOGLE_FONTS_FONT = "https://fonts.gstatic.com";

function parseCSPExtraSources(rawValue) {
  return String(rawValue ?? "")
    .split(/[\s,]+/)
    .map((value) => value.trim())
    .filter(Boolean);
}

function mergeCSPDirectiveSources(baseSources, extraSourcesRaw) {
  return [
    ...new Set([...baseSources, ...parseCSPExtraSources(extraSourcesRaw)]),
  ];
}

function buildCSPDirectives(env = privateEnv) {
  const scriptSrc = dev
    ? ["'self'", "'unsafe-inline'", "'unsafe-eval'"]
    : ["'self'"];

  return {
    "default-src": ["'self'"],
    "script-src": mergeCSPDirectiveSources(
      scriptSrc,
      env.OAR_UI_CSP_SCRIPT_SRC_EXTRA,
    ),
    "style-src": mergeCSPDirectiveSources(
      ["'self'", "'unsafe-inline'", GOOGLE_FONTS_STYLE],
      env.OAR_UI_CSP_STYLE_SRC_EXTRA,
    ),
    "img-src": mergeCSPDirectiveSources(
      ["'self'", "data:", "https:"],
      env.OAR_UI_CSP_IMG_SRC_EXTRA,
    ),
    "font-src": mergeCSPDirectiveSources(
      ["'self'", "data:", GOOGLE_FONTS_FONT],
      env.OAR_UI_CSP_FONT_SRC_EXTRA,
    ),
    "connect-src": mergeCSPDirectiveSources(
      ["'self'"],
      env.OAR_UI_CSP_CONNECT_SRC_EXTRA,
    ),
    "manifest-src": mergeCSPDirectiveSources(
      ["'self'"],
      env.OAR_UI_CSP_MANIFEST_SRC_EXTRA,
    ),
    "frame-ancestors": ["'none'"],
    "base-uri": ["'self'"],
    "form-action": ["'self'"],
    "object-src": ["'none'"],
  };
}

function buildCSPHeader() {
  return Object.entries(buildCSPDirectives())
    .map(([directive, values]) => `${directive} ${values.join(" ")}`)
    .join("; ");
}

export async function handle({ event, resolve }) {
  const pathname = stripBasePath(event.url.pathname);
  const method = event.request.method;
  const documentNavigation = isDocumentNavigationRequest(event.request);
  const proxyableRequest =
    (isProxyableCommand(method, pathname) ||
      isDirectCoreProxyPath(method, pathname)) &&
    !documentNavigation &&
    !shouldBypassProxy(pathname, method);

  if (proxyableRequest) {
    const target = await resolveProxyTarget({
      event,
      workspaceSlug: getWorkspaceHeader(event.request.headers),
    });
    if (target.status) {
      return new Response(JSON.stringify(target.payload), {
        status: target.status,
        headers: {
          "content-type": "application/json",
          "X-OAR-UI-Version": CURRENT_VERSION,
        },
      });
    }

    if (target.coreBaseUrl) {
      const response = await proxyToCore(
        event,
        target.coreBaseUrl,
        target.workspace.slug,
      );
      response.headers.set("X-OAR-UI-Version", CURRENT_VERSION);
      return response;
    }

    return new Response(
      JSON.stringify({
        error: {
          code: "core_not_configured",
          message:
            "Workspace is configured but coreBaseUrl is missing. Set OAR_CORE_BASE_URL or add coreBaseUrl to OAR_WORKSPACES for this workspace.",
        },
      }),
      {
        status: 503,
        headers: {
          "content-type": "application/json",
          "X-OAR-UI-Version": CURRENT_VERSION,
        },
      },
    );
  }

  const response = await resolve(event);
  response.headers.set("X-OAR-UI-Version", CURRENT_VERSION);

  if (documentNavigation) {
    response.headers.set("Content-Security-Policy", buildCSPHeader());
    response.headers.set("X-Frame-Options", "DENY");
    response.headers.set("X-Content-Type-Options", "nosniff");
    response.headers.set("Referrer-Policy", "strict-origin-when-cross-origin");
  }

  return response;
}
