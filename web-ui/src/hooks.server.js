import { dev } from "$app/environment";
import { isProxyableCommand } from "$lib/coreRouteCatalog";
import { getWorkspaceHeader } from "$lib/compat/workspaceCompat";
import { stripBasePath } from "$lib/workspacePaths";
import {
  clearWorkspaceAuthSession,
  getWorkspaceAuthSession,
  readWorkspaceRefreshToken,
  refreshWorkspaceAuthSession,
} from "$lib/server/authSession";
import { buildProxyRequestInit } from "$lib/server/coreProxy";
import { resolveProxyTarget } from "$lib/server/proxyWorkspaceTarget";

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
  } catch {
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

  const requestInit = buildProxyRequestInit(event, {
    body: requestBody,
  });
  requestInit.headers.delete("cookie");
  requestInit.headers.delete("authorization");

  const session = getWorkspaceAuthSession(event, workspaceSlug);
  if (session?.accessToken) {
    requestInit.headers.set("authorization", `Bearer ${session.accessToken}`);
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

function buildCSPDirectives() {
  const scriptSrc = dev
    ? ["'self'", "'unsafe-inline'", "'unsafe-eval'"]
    : ["'self'"];

  return {
    "default-src": ["'self'"],
    "script-src": scriptSrc,
    "style-src": ["'self'", "'unsafe-inline'", GOOGLE_FONTS_STYLE],
    "img-src": ["'self'", "data:", "https:"],
    "font-src": ["'self'", "data:", GOOGLE_FONTS_FONT],
    "connect-src": ["'self'"],
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
    isProxyableCommand(method, pathname) &&
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
        },
      });
    }

    if (target.coreBaseUrl) {
      return proxyToCore(event, target.coreBaseUrl, target.workspace.slug);
    }
  }

  const response = await resolve(event);

  if (documentNavigation) {
    response.headers.set("Content-Security-Policy", buildCSPHeader());
    response.headers.set("X-Frame-Options", "DENY");
    response.headers.set("X-Content-Type-Options", "nosniff");
    response.headers.set("Referrer-Policy", "strict-origin-when-cross-origin");
  }

  return response;
}
