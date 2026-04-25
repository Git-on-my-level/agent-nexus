import { json } from "@sveltejs/kit";

import { AuthErrorCode } from "$lib/authErrorCodes.js";
import { buildProxyRequestInit } from "./coreProxy.js";
import { resolveWorkspaceFromEvent } from "./workspaceResolver.js";
import { WORKSPACE_HEADER, normalizeWorkspaceSlug } from "../workspacePaths.js";

function getWorkspaceSlug(value) {
  const slug = normalizeWorkspaceSlug(value);
  if (!slug) {
    throw new Error("workspace slug is required for auth session");
  }
  return slug;
}

/**
 * In-memory dedup window for identical refresh_token reuse (see
 * `refreshWorkspaceAuthSession`). **Known limitation:** scoped to this Node
 * process only — Vite HMR or a dev-server restart clears it. Do not "fix"
 * transient 401s by adding blind retries; see `web-ui/AGENTS.md` (auth).
 */
const REFRESH_REPLAY_WINDOW_MS = 60_000;
const ACCESS_TOKEN_TTL_SECONDS = 15 * 60;
const REFRESH_TOKEN_COOKIE_MAX_AGE_SECONDS = 30 * 24 * 60 * 60;
const RETRYABLE_AUTH_SESSION_FAILURE_COOKIE_MAX_AGE_SECONDS = 2 * 60;
const RETRYABLE_AUTH_SESSION_FAILURE_MAX_ATTEMPTS = 2;
export const RETRYABLE_AUTH_SESSION_ERROR_CODE = "auth_session_retryable";
// Retain the last access token only slightly beyond its real core TTL so
// refresh-race detection can still tell "stale token after rotation" apart
// from "no prior access token", without preserving stale-auth state for the
// entire refresh-token lifetime.
const ACCESS_TOKEN_COOKIE_MAX_AGE_SECONDS =
  ACCESS_TOKEN_TTL_SECONDS + Math.ceil(REFRESH_REPLAY_WINDOW_MS / 1000);
const inFlightRefreshes = new Map();
const recentRefreshResults = new Map();

export function getAuthSessionCookieName(workspaceSlug) {
  return `anx_ui_session_${getWorkspaceSlug(workspaceSlug)}`;
}

export function getAuthAccessCookieName(workspaceSlug) {
  return `anx_ui_access_${getWorkspaceSlug(workspaceSlug)}`;
}

function getRetryableAuthFailureCookieName(workspaceSlug) {
  return `anx_ui_auth_retry_${getWorkspaceSlug(workspaceSlug)}`;
}

function isSecureCookieRequest(event) {
  return event.url.protocol === "https:";
}

function buildAuthSessionCookieOptions(event, { maxAge } = {}) {
  return {
    httpOnly: true,
    sameSite: "lax",
    secure: isSecureCookieRequest(event),
    path: "/",
    ...(Number.isFinite(maxAge) ? { maxAge } : {}),
  };
}

function readJSONPayload(rawText) {
  const text = String(rawText ?? "").trim();
  if (!text) {
    return {};
  }

  try {
    return JSON.parse(text);
  } catch {
    return { message: text };
  }
}

function createRequestError(status, payload) {
  const message =
    payload?.error?.message || payload?.message || `request failed (${status})`;
  const error = new Error(message);
  error.status = status;
  error.details = payload;
  return error;
}

function createRetryableAuthSessionError(error) {
  const retryableError = new Error(
    "Workspace authentication refresh is in progress. Retry shortly.",
  );
  retryableError.status = 503;
  retryableError.code = RETRYABLE_AUTH_SESSION_ERROR_CODE;
  retryableError.details = error?.details ?? null;
  return retryableError;
}

async function requestCoreJSON(coreBaseUrl, pathname, options = {}) {
  const url = new URL(pathname, `${coreBaseUrl}/`).toString();
  const response = await fetch(url, {
    method: options.method ?? "GET",
    headers: {
      accept: "application/json",
      ...(options.body ? { "content-type": "application/json" } : {}),
      ...(options.token ? { authorization: `Bearer ${options.token}` } : {}),
    },
    body: options.body ? JSON.stringify(options.body) : undefined,
  });

  const payload = readJSONPayload(await response.text());
  if (!response.ok) {
    throw createRequestError(response.status, payload);
  }

  return payload;
}

function getRefreshDeduplicationKey(workspaceSlug, refreshToken) {
  return `${getWorkspaceSlug(workspaceSlug)}:${String(refreshToken ?? "").trim()}`;
}

function purgeExpiredRecentRefreshResults(now = Date.now()) {
  for (const [key, cached] of recentRefreshResults.entries()) {
    if (now >= cached.expiresAt) {
      recentRefreshResults.delete(key);
    }
  }
}

function readRecentRefreshResult(key) {
  const cached = recentRefreshResults.get(key);
  if (!cached) {
    return null;
  }
  if (Date.now() >= cached.expiresAt) {
    recentRefreshResults.delete(key);
    return null;
  }
  return cached.result;
}

function applyRefreshResult(event, workspaceSlug, tokens) {
  if (!tokens) {
    return null;
  }
  clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);
  if (tokens.refreshToken) {
    writeWorkspaceRefreshToken(event, workspaceSlug, tokens.refreshToken);
  }
  if (tokens.accessToken) {
    writeWorkspaceAccessToken(event, workspaceSlug, tokens.accessToken);
  }
  return tokens;
}

function cacheRecentRefreshResult(key, tokens) {
  purgeExpiredRecentRefreshResults();
  recentRefreshResults.set(key, {
    expiresAt: Date.now() + REFRESH_REPLAY_WINDOW_MS,
    result: tokens,
  });
}

export function readWorkspaceAccessToken(event, workspaceSlug) {
  return event.cookies.get(getAuthAccessCookieName(workspaceSlug)) ?? "";
}

function assertCookieSlugMatchesRouteWorkspace(event, workspaceSlug) {
  const routeWs = event?.params?.workspace;
  if (!routeWs || typeof routeWs !== "string") {
    return;
  }
  const fromRoute = normalizeWorkspaceSlug(routeWs);
  const fromArg = normalizeWorkspaceSlug(workspaceSlug);
  if (!fromRoute || !fromArg || fromRoute === fromArg) {
    return;
  }
  const msg = `[auth] cookie write for workspace slug "${fromArg}" but route param workspace is "${fromRoute}"`;
  if (process.env.VITEST || process.env.NODE_ENV === "test") {
    throw new Error(msg);
  }
  if (import.meta.env.DEV) {
    console.warn(msg);
  }
}

export function writeWorkspaceAccessToken(event, workspaceSlug, accessToken) {
  const normalized = String(accessToken ?? "").trim();
  if (!normalized) {
    clearWorkspaceAccessToken(event, workspaceSlug);
    return;
  }

  assertCookieSlugMatchesRouteWorkspace(event, workspaceSlug);
  event.cookies.set(
    getAuthAccessCookieName(workspaceSlug),
    normalized,
    buildAuthSessionCookieOptions(event, {
      maxAge: ACCESS_TOKEN_COOKIE_MAX_AGE_SECONDS,
    }),
  );
}

export function clearWorkspaceAccessToken(event, workspaceSlug) {
  event.cookies.delete(getAuthAccessCookieName(workspaceSlug), {
    path: "/",
  });
}

export function getWorkspaceAuthSession(event, workspaceSlug) {
  const refreshToken = readWorkspaceRefreshToken(event, workspaceSlug);
  const accessToken = readWorkspaceAccessToken(event, workspaceSlug);
  if (!refreshToken && !accessToken) {
    return null;
  }

  return {
    refreshToken,
    accessToken,
  };
}

export function clearWorkspaceAuthSession(event, workspaceSlug) {
  clearWorkspaceRefreshToken(event, workspaceSlug);
  clearWorkspaceAccessToken(event, workspaceSlug);
  clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);
}

/** Account status ended the account/session (inactive, revoked). Maps to SessionEndedOverlay. */
export function isTerminalAccountSessionFailure(error) {
  const code = error?.details?.error?.code;
  return (
    code === AuthErrorCode.SESSION_ENDED_BY_ACCOUNT_STATUS ||
    code === AuthErrorCode.CP_ACCOUNT_INACTIVE
  );
}

export function readWorkspaceRefreshToken(event, workspaceSlug) {
  return event.cookies.get(getAuthSessionCookieName(workspaceSlug)) ?? "";
}

export function writeWorkspaceRefreshToken(event, workspaceSlug, refreshToken) {
  const normalized = String(refreshToken ?? "").trim();
  if (!normalized) {
    clearWorkspaceRefreshToken(event, workspaceSlug);
    return;
  }

  assertCookieSlugMatchesRouteWorkspace(event, workspaceSlug);
  event.cookies.set(
    getAuthSessionCookieName(workspaceSlug),
    normalized,
    buildAuthSessionCookieOptions(event, {
      maxAge: REFRESH_TOKEN_COOKIE_MAX_AGE_SECONDS,
    }),
  );
}

export function clearWorkspaceRefreshToken(event, workspaceSlug) {
  event.cookies.delete(getAuthSessionCookieName(workspaceSlug), {
    path: "/",
  });
}

function readRetryableWorkspaceAuthFailureCount(event, workspaceSlug) {
  const raw = event.cookies.get(
    getRetryableAuthFailureCookieName(workspaceSlug),
  );
  const count = Number.parseInt(String(raw ?? "").trim(), 10);
  return Number.isFinite(count) && count > 0 ? count : 0;
}

function writeRetryableWorkspaceAuthFailureCount(event, workspaceSlug, count) {
  if (!Number.isInteger(count) || count <= 0) {
    clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);
    return;
  }

  event.cookies.set(
    getRetryableAuthFailureCookieName(workspaceSlug),
    String(count),
    buildAuthSessionCookieOptions(event, {
      maxAge: RETRYABLE_AUTH_SESSION_FAILURE_COOKIE_MAX_AGE_SECONDS,
    }),
  );
}

export function clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug) {
  event.cookies.delete(getRetryableAuthFailureCookieName(workspaceSlug), {
    path: "/",
  });
}

export function shouldClearWorkspaceAuthSessionAfterRetryableFailure(
  event,
  workspaceSlug,
) {
  const nextCount =
    readRetryableWorkspaceAuthFailureCount(event, workspaceSlug) + 1;
  if (nextCount >= RETRYABLE_AUTH_SESSION_FAILURE_MAX_ATTEMPTS) {
    clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);
    return true;
  }

  writeRetryableWorkspaceAuthFailureCount(event, workspaceSlug, nextCount);
  return false;
}

export async function resolveWorkspaceSlugFromEvent(event) {
  const resolved = await resolveWorkspaceFromEvent(event);
  return {
    ...resolved,
    workspaceSlug: getWorkspaceSlug(resolved.workspaceSlug),
  };
}

export async function refreshWorkspaceAuthSession({
  event,
  workspaceSlug,
  coreBaseUrl,
}) {
  if (!coreBaseUrl) {
    return null;
  }

  const refreshToken = readWorkspaceRefreshToken(event, workspaceSlug);
  if (!refreshToken) {
    clearWorkspaceAuthSession(event, workspaceSlug);
    return null;
  }

  const dedupeKey = getRefreshDeduplicationKey(workspaceSlug, refreshToken);
  const recentResult = readRecentRefreshResult(dedupeKey);
  if (recentResult) {
    return applyRefreshResult(event, workspaceSlug, recentResult);
  }

  const inFlightRefresh = inFlightRefreshes.get(dedupeKey);
  if (inFlightRefresh) {
    return applyRefreshResult(event, workspaceSlug, await inFlightRefresh);
  }

  const refreshPromise = requestCoreJSON(coreBaseUrl, "/auth/token", {
    method: "POST",
    body: {
      grant_type: "refresh_token",
      refresh_token: refreshToken,
    },
  })
    .then((tokenResponse) => {
      const nextTokens = tokenResponse.tokens ?? {};
      const nextRefreshToken =
        String(nextTokens.refresh_token ?? "").trim() || refreshToken;
      const accessToken = String(nextTokens.access_token ?? "").trim();

      if (!accessToken) {
        throw createRequestError(502, {
          message: "anx-core returned an empty access token.",
        });
      }

      const issuedTokens = {
        refreshToken: nextRefreshToken,
        accessToken,
      };
      cacheRecentRefreshResult(dedupeKey, issuedTokens);
      return issuedTokens;
    })
    .finally(() => {
      inFlightRefreshes.delete(dedupeKey);
    });

  inFlightRefreshes.set(dedupeKey, refreshPromise);

  return applyRefreshResult(event, workspaceSlug, await refreshPromise);
}

export function isLikelyStaleWorkspaceRefreshFailure(
  error,
  { hadAccessToken = false } = {},
) {
  return (
    hadAccessToken &&
    error?.status === 401 &&
    error?.details?.error?.code === "invalid_token"
  );
}

export function isRetryableWorkspaceRefreshFailure(
  error,
  { hadAccessToken = false, hadRefreshToken = false } = {},
) {
  return (
    error?.status === 401 &&
    error?.details?.error?.code === "invalid_token" &&
    (hadAccessToken || hadRefreshToken)
  );
}

export function isRetryableWorkspaceAuthSessionError(error) {
  return (
    error?.status === 503 && error?.code === RETRYABLE_AUTH_SESSION_ERROR_CODE
  );
}

export async function loadWorkspaceAuthenticatedAgent({
  event,
  workspaceSlug,
  coreBaseUrl,
}) {
  if (!coreBaseUrl) {
    return null;
  }

  const refreshToken = readWorkspaceRefreshToken(event, workspaceSlug);
  let accessToken = readWorkspaceAccessToken(event, workspaceSlug);

  if (!refreshToken && !accessToken) {
    clearWorkspaceAuthSession(event, workspaceSlug);
    return null;
  }

  async function fetchCurrentAgent(token) {
    const agentResponse = await requestCoreJSON(coreBaseUrl, "/agents/me", {
      token,
    });
    return agentResponse.agent ?? null;
  }

  if (accessToken) {
    try {
      const agent = await fetchCurrentAgent(accessToken);
      clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);
      return agent;
    } catch (error) {
      if (isTerminalAccountSessionFailure(error)) {
        throw error;
      }
      if (error?.status !== 401) {
        throw error;
      }
      if (!refreshToken) {
        clearWorkspaceAuthSession(event, workspaceSlug);
        return null;
      }
    }
  }

  if (!refreshToken) {
    return null;
  }

  try {
    await refreshWorkspaceAuthSession({
      event,
      workspaceSlug,
      coreBaseUrl,
    });
    accessToken = readWorkspaceAccessToken(event, workspaceSlug);
    if (!accessToken) {
      return null;
    }
    const agent = await fetchCurrentAgent(accessToken);
    clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);
    return agent;
  } catch (error) {
    if (isTerminalAccountSessionFailure(error)) {
      throw error;
    }
    if (
      isRetryableWorkspaceRefreshFailure(error, {
        hadAccessToken: Boolean(accessToken),
        hadRefreshToken: Boolean(refreshToken),
      })
    ) {
      if (
        shouldClearWorkspaceAuthSessionAfterRetryableFailure(
          event,
          workspaceSlug,
        )
      ) {
        clearWorkspaceAuthSession(event, workspaceSlug);
        return null;
      }
      throw createRetryableAuthSessionError(error);
    }
    if (error?.status === 401) {
      clearWorkspaceAuthSession(event, workspaceSlug);
      return null;
    }
    throw error;
  }
}

export async function handleWorkspaceAuthVerifyResponse({
  event,
  workspaceSlug,
  upstreamResponse,
}) {
  const responseHeaders = new Headers(upstreamResponse.headers);
  responseHeaders.delete("content-length");
  responseHeaders.delete("content-encoding");

  const payload = readJSONPayload(
    await upstreamResponse.text().catch(() => ""),
  );
  if (!upstreamResponse.ok) {
    return new Response(JSON.stringify(payload), {
      status: upstreamResponse.status,
      statusText: upstreamResponse.statusText,
      headers: {
        ...Object.fromEntries(responseHeaders.entries()),
        "cache-control": "no-store",
      },
    });
  }

  const tokens = payload.tokens ?? {};
  const refreshToken = String(tokens.refresh_token ?? "").trim();
  const accessToken = String(tokens.access_token ?? "").trim();
  const agent = payload.agent ?? null;

  if (refreshToken) {
    writeWorkspaceRefreshToken(event, workspaceSlug, refreshToken);
  }
  if (accessToken) {
    writeWorkspaceAccessToken(event, workspaceSlug, accessToken);
  }
  clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);

  const sanitizedPayload = {
    agent,
  };

  return json(sanitizedPayload, {
    status: upstreamResponse.status,
    headers: {
      ...Object.fromEntries(responseHeaders.entries()),
      "cache-control": "no-store",
    },
  });
}

export async function proxyWorkspaceAuthVerify({
  event,
  workspaceSlug,
  coreBaseUrl,
  pathname,
}) {
  const targetUrl = new URL(pathname, `${coreBaseUrl}/`).toString();
  const requestInit = buildProxyRequestInit(event);
  requestInit.headers.delete("cookie");
  requestInit.headers.delete("authorization");
  requestInit.headers.delete(WORKSPACE_HEADER);

  const upstreamResponse = await fetch(targetUrl, requestInit);
  return handleWorkspaceAuthVerifyResponse({
    event,
    workspaceSlug,
    upstreamResponse,
  });
}

export function resetWorkspaceAuthRefreshStateForTests() {
  inFlightRefreshes.clear();
  recentRefreshResults.clear();
}

export function getRecentRefreshResultCountForTests() {
  purgeExpiredRecentRefreshResults();
  return recentRefreshResults.size;
}
