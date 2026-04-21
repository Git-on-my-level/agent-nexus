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
import { logServerError, logServerEvent } from "$lib/server/devLog";
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

function isSvelteKitDataFetch(pathname) {
  return pathname.endsWith("/__data.json");
}

// Sliding-window per-URL request counter. In dev we want to surface tight
// fetch loops (e.g. a client-side $effect repeatedly calling goto() against a
// route that triggers a server load). When the same method+path fires more
// than `LOOP_THRESHOLD` times within `LOOP_WINDOW_MS`, we flag it once so
// the loop is impossible to miss in the dev terminal.
const LOOP_WINDOW_MS = 2000;
const LOOP_THRESHOLD = 8;
const LOOP_FLAG_COOLDOWN_MS = 5000;
/** @type {Map<string, {timestamps: number[], lastFlaggedAt: number}>} */
const loopTracker = new Map();

function trackRequestForLoopDetection(key) {
  const now = Date.now();
  let entry = loopTracker.get(key);
  if (!entry) {
    entry = { timestamps: [], lastFlaggedAt: 0 };
    loopTracker.set(key, entry);
  }
  entry.timestamps.push(now);
  while (
    entry.timestamps.length > 0 &&
    now - entry.timestamps[0] > LOOP_WINDOW_MS
  ) {
    entry.timestamps.shift();
  }
  if (
    entry.timestamps.length >= LOOP_THRESHOLD &&
    now - entry.lastFlaggedAt > LOOP_FLAG_COOLDOWN_MS
  ) {
    entry.lastFlaggedAt = now;
    return entry.timestamps.length;
  }
  return 0;
}

// Exposed for tests so we can reset state between cases.
export function __resetLoopTrackerForTests() {
  loopTracker.clear();
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
          message: `Unable to reach anx-core at ${coreBaseUrl}. Start backend with ../core/scripts/dev and retry.`,
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
      env.ANX_UI_CSP_SCRIPT_SRC_EXTRA,
    ),
    "style-src": mergeCSPDirectiveSources(
      ["'self'", "'unsafe-inline'", GOOGLE_FONTS_STYLE],
      env.ANX_UI_CSP_STYLE_SRC_EXTRA,
    ),
    "img-src": mergeCSPDirectiveSources(
      ["'self'", "data:", "https:"],
      env.ANX_UI_CSP_IMG_SRC_EXTRA,
    ),
    "font-src": mergeCSPDirectiveSources(
      ["'self'", "data:", GOOGLE_FONTS_FONT],
      env.ANX_UI_CSP_FONT_SRC_EXTRA,
    ),
    "connect-src": mergeCSPDirectiveSources(
      ["'self'", GOOGLE_FONTS_STYLE, GOOGLE_FONTS_FONT],
      env.ANX_UI_CSP_CONNECT_SRC_EXTRA,
    ),
    "manifest-src": mergeCSPDirectiveSources(
      ["'self'"],
      env.ANX_UI_CSP_MANIFEST_SRC_EXTRA,
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
          "X-ANX-UI-Version": CURRENT_VERSION,
        },
      });
    }

    if (target.coreBaseUrl) {
      const response = await proxyToCore(
        event,
        target.coreBaseUrl,
        target.workspace.slug,
      );
      response.headers.set("X-ANX-UI-Version", CURRENT_VERSION);
      return response;
    }

    return new Response(
      JSON.stringify({
        error: {
          code: "core_not_configured",
          message:
            "Workspace is configured but coreBaseUrl is missing. Set ANX_CORE_BASE_URL or add coreBaseUrl to ANX_WORKSPACES for this workspace.",
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

  const dataFetch = isSvelteKitDataFetch(pathname);
  // Log document navigations and SvelteKit data fetches (`__data.json`).
  // The data-fetch case is critical: when the client navigates between
  // routes, only the data fetch hits the server — and a runaway client-side
  // `goto()` loop manifests as repeated `__data.json` requests rather than
  // full document navigations. Logging both makes loops visible in the dev
  // terminal even when no full-page reload happens.
  const shouldLog = dev && (documentNavigation || dataFetch);
  const requestStart = shouldLog ? Date.now() : 0;
  const response = await resolve(event);
  response.headers.set("X-ANX-UI-Version", CURRENT_VERSION);

  if (documentNavigation) {
    response.headers.set("Content-Security-Policy", buildCSPHeader());
    response.headers.set("X-Frame-Options", "DENY");
    response.headers.set("X-Content-Type-Options", "nosniff");
    response.headers.set("Referrer-Policy", "strict-origin-when-cross-origin");
  }

  if (shouldLog) {
    const status = response.status;
    const kind = documentNavigation ? "ssr" : "data";
    const fields = {
      kind,
      method,
      url: event.url.pathname + event.url.search,
      route: event.route?.id ?? "(unknown)",
      status,
      duration_ms: Date.now() - requestStart,
    };
    logServerEvent("ssr.request", fields, {
      level: status >= 400 ? "warn" : "info",
    });

    const loopKey = `${method} ${event.url.pathname}`;
    const loopCount = trackRequestForLoopDetection(loopKey);
    if (loopCount > 0) {
      logServerEvent(
        "ssr.request.loop_detected",
        {
          method,
          path: event.url.pathname,
          count: loopCount,
          window_ms: LOOP_WINDOW_MS,
          hint: "Possible runaway client-side goto()/load() loop. Check shouldRedirectToLogin in +layout.svelte and any $effect that calls goto().",
        },
        { level: "warn" },
      );
    }
  }

  return response;
}

/**
 * SvelteKit `handleError` hook. Triggered when an exception bubbles out of a
 * load(), action, server endpoint, or hook (anything that's NOT a
 * `throw error(status, msg)` — those are handled via the normal error
 * response and routed to +error.svelte).
 *
 * Without this hook, real exceptions vanish into a generic 500 page with
 * minimal terminal output, which is the opaqueness the user complained
 * about. Always log structured context AND attach a `code` so the error
 * page can render something useful.
 *
 * @type {import("@sveltejs/kit").HandleServerError}
 */
export function handleError({ error, event, status, message }) {
  logServerError("ssr.unhandled_error", error, {
    route: event?.route?.id ?? "(unknown)",
    method: event?.request?.method,
    url: event?.url ? event.url.pathname + event.url.search : undefined,
    status,
    sveltekit_message: message,
  });
  return {
    message:
      message || (error instanceof Error ? error.message : String(error)),
    code: "ssr_unhandled_error",
  };
}
