import { json } from "@sveltejs/kit";

import { AuthErrorCode } from "$lib/authErrorCodes.js";
import { coreBaseUrlForNodeFetch } from "./coreBaseUrlForNodeFetch.js";
import { coreEndpointURL } from "./coreEndpoint.js";
import { buildProxyRequestInit } from "./coreProxy.js";
import { resolveWorkspaceFromEvent } from "./workspaceResolver.js";
import {
  WORKSPACE_HEADER,
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
  workspaceCompositeKey,
} from "../workspacePaths.js";

function getWorkspaceSlug(value) {
  const slug = normalizeWorkspaceSlug(value);
  if (!slug) {
    throw new Error("workspace slug is required for auth session");
  }
  return slug;
}

function getOrganizationSlug(value) {
  const slug = normalizeOrganizationSlug(value);
  if (!slug) {
    throw new Error("organization slug is required for auth session");
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

/**
 * @param {string} organizationSlug
 * @param {string} workspaceSlug
 */
export function getAuthSessionCookieName(organizationSlug, workspaceSlug) {
  const org = getOrganizationSlug(organizationSlug);
  const ws = getWorkspaceSlug(workspaceSlug);
  return `anx_ui_session_${org}__${ws}`;
}

/**
 * @param {string} organizationSlug
 * @param {string} workspaceSlug
 */
export function getAuthAccessCookieName(organizationSlug, workspaceSlug) {
  const org = getOrganizationSlug(organizationSlug);
  const ws = getWorkspaceSlug(workspaceSlug);
  return `anx_ui_access_${org}__${ws}`;
}

function getRetryableAuthFailureCookieName(organizationSlug, workspaceSlug) {
  const org = getOrganizationSlug(organizationSlug);
  const ws = getWorkspaceSlug(workspaceSlug);
  return `anx_ui_auth_retry_${org}__${ws}`;
}

/** Pre–org+workspace scoping; REMOVABLE after migration period. */
function getLegacyAuthSessionCookieName(workspaceSlug) {
  return `anx_ui_session_${getWorkspaceSlug(workspaceSlug)}`;
}

function getLegacyAuthAccessCookieName(workspaceSlug) {
  return `anx_ui_access_${getWorkspaceSlug(workspaceSlug)}`;
}

function getLegacyRetryableAuthFailureCookieName(workspaceSlug) {
  return `anx_ui_auth_retry_${getWorkspaceSlug(workspaceSlug)}`;
}

/**
 * If legacy workspace-only cookies exist for this workspace slug, copy into
 * org+workspace-scoped names and clear legacy. REMOVABLE after migration period.
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {string} organizationSlug
 * @param {string} workspaceSlug
 */
function maybeMigrateWorkspaceAuthCookiesFromLegacy(
  event,
  organizationSlug,
  workspaceSlug,
) {
  const org = getOrganizationSlug(organizationSlug);
  const ws = getWorkspaceSlug(workspaceSlug);
  const nextSession = getAuthSessionCookieName(org, ws);
  if (event.cookies.get(nextSession)) {
    return;
  }
  const legRefresh = getLegacyAuthSessionCookieName(ws);
  const legAccess = getLegacyAuthAccessCookieName(ws);
  const legRetry = getLegacyRetryableAuthFailureCookieName(ws);
  const refreshVal = event.cookies.get(legRefresh) ?? "";
  const accessVal = event.cookies.get(legAccess) ?? "";
  const retryVal = event.cookies.get(legRetry) ?? "";
  if (!refreshVal && !accessVal && !retryVal) {
    return;
  }
  if (refreshVal) {
    event.cookies.set(
      nextSession,
      refreshVal,
      buildAuthSessionCookieOptions(event, {
        maxAge: REFRESH_TOKEN_COOKIE_MAX_AGE_SECONDS,
      }),
    );
  }
  if (accessVal) {
    event.cookies.set(
      getAuthAccessCookieName(org, ws),
      accessVal,
      buildAuthSessionCookieOptions(event, {
        maxAge: ACCESS_TOKEN_COOKIE_MAX_AGE_SECONDS,
      }),
    );
  }
  const retryCount = Number.parseInt(String(retryVal ?? "").trim(), 10);
  if (Number.isFinite(retryCount) && retryCount > 0) {
    event.cookies.set(
      getRetryableAuthFailureCookieName(org, ws),
      String(retryCount),
      buildAuthSessionCookieOptions(event, {
        maxAge: RETRYABLE_AUTH_SESSION_FAILURE_COOKIE_MAX_AGE_SECONDS,
      }),
    );
  }
  event.cookies.delete(legRefresh, { path: "/" });
  event.cookies.delete(legAccess, { path: "/" });
  event.cookies.delete(legRetry, { path: "/" });
}

function clearLegacyWorkspaceAuthCookiesForSlug(event, workspaceSlug) {
  const ws = getWorkspaceSlug(workspaceSlug);
  event.cookies.delete(getLegacyAuthSessionCookieName(ws), { path: "/" });
  event.cookies.delete(getLegacyAuthAccessCookieName(ws), { path: "/" });
  event.cookies.delete(getLegacyRetryableAuthFailureCookieName(ws), {
    path: "/",
  });
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
  const url = coreEndpointURL(coreBaseUrlForNodeFetch(coreBaseUrl), pathname);
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

function getRefreshDeduplicationKey(
  organizationSlug,
  workspaceSlug,
  refreshToken,
) {
  const composite = workspaceCompositeKey(
    getOrganizationSlug(organizationSlug),
    getWorkspaceSlug(workspaceSlug),
  );
  return `${composite}:${String(refreshToken ?? "").trim()}`;
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

function applyRefreshResult(event, organizationSlug, workspaceSlug, tokens) {
  if (!tokens) {
    return null;
  }
  clearRetryableWorkspaceAuthFailureCount(
    event,
    organizationSlug,
    workspaceSlug,
  );
  if (tokens.refreshToken) {
    writeWorkspaceRefreshToken(
      event,
      organizationSlug,
      workspaceSlug,
      tokens.refreshToken,
    );
  }
  if (tokens.accessToken) {
    writeWorkspaceAccessToken(
      event,
      organizationSlug,
      workspaceSlug,
      tokens.accessToken,
    );
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

export function readWorkspaceAccessToken(
  event,
  organizationSlug,
  workspaceSlug,
) {
  return (
    event.cookies.get(
      getAuthAccessCookieName(organizationSlug, workspaceSlug),
    ) ?? ""
  );
}

function assertSessionIdentityMatchesRoute(
  event,
  organizationSlug,
  workspaceSlug,
) {
  const routeWs = event?.params?.workspace;
  if (routeWs && typeof routeWs === "string") {
    const fromRoute = normalizeWorkspaceSlug(routeWs);
    const fromArg = normalizeWorkspaceSlug(workspaceSlug);
    if (fromRoute && fromArg && fromRoute !== fromArg) {
      const msg = `[auth] cookie write for workspace slug "${fromArg}" but route param workspace is "${fromRoute}"`;
      if (process.env.VITEST || process.env.NODE_ENV === "test") {
        throw new Error(msg);
      }
      if (import.meta.env.DEV) {
        console.warn(msg);
      }
    }
  }
  const routeOrg = event?.params?.organization;
  if (routeOrg && typeof routeOrg === "string") {
    const fromRoute = normalizeOrganizationSlug(routeOrg);
    const fromArg = normalizeOrganizationSlug(organizationSlug);
    if (fromRoute && fromArg && fromRoute !== fromArg) {
      const msg = `[auth] cookie write for org slug "${fromArg}" but route param organization is "${fromRoute}"`;
      if (process.env.VITEST || process.env.NODE_ENV === "test") {
        throw new Error(msg);
      }
      if (import.meta.env.DEV) {
        console.warn(msg);
      }
    }
  }
}

export function writeWorkspaceAccessToken(
  event,
  organizationSlug,
  workspaceSlug,
  accessToken,
) {
  const normalized = String(accessToken ?? "").trim();
  if (!normalized) {
    clearWorkspaceAccessToken(event, organizationSlug, workspaceSlug);
    return;
  }

  assertSessionIdentityMatchesRoute(event, organizationSlug, workspaceSlug);
  event.cookies.set(
    getAuthAccessCookieName(organizationSlug, workspaceSlug),
    normalized,
    buildAuthSessionCookieOptions(event, {
      maxAge: ACCESS_TOKEN_COOKIE_MAX_AGE_SECONDS,
    }),
  );
  clearLegacyWorkspaceAuthCookiesForSlug(event, workspaceSlug);
}

export function clearWorkspaceAccessToken(
  event,
  organizationSlug,
  workspaceSlug,
) {
  event.cookies.delete(
    getAuthAccessCookieName(organizationSlug, workspaceSlug),
    {
      path: "/",
    },
  );
}

export function getWorkspaceAuthSession(
  event,
  organizationSlug,
  workspaceSlug,
) {
  const refreshToken = readWorkspaceRefreshToken(
    event,
    organizationSlug,
    workspaceSlug,
  );
  const accessToken = readWorkspaceAccessToken(
    event,
    organizationSlug,
    workspaceSlug,
  );
  if (!refreshToken && !accessToken) {
    return null;
  }

  return {
    refreshToken,
    accessToken,
  };
}

export function clearWorkspaceAuthSession(
  event,
  organizationSlug,
  workspaceSlug,
) {
  clearWorkspaceRefreshToken(event, organizationSlug, workspaceSlug);
  clearWorkspaceAccessToken(event, organizationSlug, workspaceSlug);
  clearRetryableWorkspaceAuthFailureCount(
    event,
    organizationSlug,
    workspaceSlug,
  );
  clearLegacyWorkspaceAuthCookiesForSlug(event, workspaceSlug);
}

/** Account status ended the account/session (inactive, revoked). Maps to SessionEndedOverlay. */
export function isTerminalAccountSessionFailure(error) {
  const code = error?.details?.error?.code;
  return (
    code === AuthErrorCode.SESSION_ENDED_BY_ACCOUNT_STATUS ||
    code === AuthErrorCode.CP_ACCOUNT_INACTIVE
  );
}

export function readWorkspaceRefreshToken(
  event,
  organizationSlug,
  workspaceSlug,
) {
  maybeMigrateWorkspaceAuthCookiesFromLegacy(
    event,
    organizationSlug,
    workspaceSlug,
  );
  return (
    event.cookies.get(
      getAuthSessionCookieName(organizationSlug, workspaceSlug),
    ) ?? ""
  );
}

export function writeWorkspaceRefreshToken(
  event,
  organizationSlug,
  workspaceSlug,
  refreshToken,
) {
  const normalized = String(refreshToken ?? "").trim();
  if (!normalized) {
    clearWorkspaceRefreshToken(event, organizationSlug, workspaceSlug);
    return;
  }

  assertSessionIdentityMatchesRoute(event, organizationSlug, workspaceSlug);
  event.cookies.set(
    getAuthSessionCookieName(organizationSlug, workspaceSlug),
    normalized,
    buildAuthSessionCookieOptions(event, {
      maxAge: REFRESH_TOKEN_COOKIE_MAX_AGE_SECONDS,
    }),
  );
  clearLegacyWorkspaceAuthCookiesForSlug(event, workspaceSlug);
}

export function clearWorkspaceRefreshToken(
  event,
  organizationSlug,
  workspaceSlug,
) {
  event.cookies.delete(
    getAuthSessionCookieName(organizationSlug, workspaceSlug),
    {
      path: "/",
    },
  );
}

function readRetryableWorkspaceAuthFailureCount(
  event,
  organizationSlug,
  workspaceSlug,
) {
  maybeMigrateWorkspaceAuthCookiesFromLegacy(
    event,
    organizationSlug,
    workspaceSlug,
  );
  const raw = event.cookies.get(
    getRetryableAuthFailureCookieName(organizationSlug, workspaceSlug),
  );
  const count = Number.parseInt(String(raw ?? "").trim(), 10);
  return Number.isFinite(count) && count > 0 ? count : 0;
}

function writeRetryableWorkspaceAuthFailureCount(
  event,
  organizationSlug,
  workspaceSlug,
  count,
) {
  if (!Number.isInteger(count) || count <= 0) {
    clearRetryableWorkspaceAuthFailureCount(
      event,
      organizationSlug,
      workspaceSlug,
    );
    return;
  }

  event.cookies.set(
    getRetryableAuthFailureCookieName(organizationSlug, workspaceSlug),
    String(count),
    buildAuthSessionCookieOptions(event, {
      maxAge: RETRYABLE_AUTH_SESSION_FAILURE_COOKIE_MAX_AGE_SECONDS,
    }),
  );
}

export function clearRetryableWorkspaceAuthFailureCount(
  event,
  organizationSlug,
  workspaceSlug,
) {
  event.cookies.delete(
    getRetryableAuthFailureCookieName(organizationSlug, workspaceSlug),
    { path: "/" },
  );
}

export function shouldClearWorkspaceAuthSessionAfterRetryableFailure(
  event,
  organizationSlug,
  workspaceSlug,
) {
  const nextCount =
    readRetryableWorkspaceAuthFailureCount(
      event,
      organizationSlug,
      workspaceSlug,
    ) + 1;
  if (nextCount >= RETRYABLE_AUTH_SESSION_FAILURE_MAX_ATTEMPTS) {
    clearRetryableWorkspaceAuthFailureCount(
      event,
      organizationSlug,
      workspaceSlug,
    );
    return true;
  }

  writeRetryableWorkspaceAuthFailureCount(
    event,
    organizationSlug,
    workspaceSlug,
    nextCount,
  );
  return false;
}

export async function resolveWorkspaceSlugFromEvent(event) {
  const resolved = await resolveWorkspaceFromEvent(event);
  if (resolved.error) {
    return resolved;
  }
  return {
    ...resolved,
    organizationSlug: getOrganizationSlug(resolved.organizationSlug),
    workspaceSlug: getWorkspaceSlug(resolved.workspaceSlug),
  };
}

export async function refreshWorkspaceAuthSession({
  event,
  organizationSlug,
  workspaceSlug,
  coreBaseUrl,
}) {
  if (!coreBaseUrl) {
    return null;
  }

  const refreshToken = readWorkspaceRefreshToken(
    event,
    organizationSlug,
    workspaceSlug,
  );
  if (!refreshToken) {
    clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
    return null;
  }

  const dedupeKey = getRefreshDeduplicationKey(
    organizationSlug,
    workspaceSlug,
    refreshToken,
  );
  const recentResult = readRecentRefreshResult(dedupeKey);
  if (recentResult) {
    return applyRefreshResult(
      event,
      organizationSlug,
      workspaceSlug,
      recentResult,
    );
  }

  const inFlightRefresh = inFlightRefreshes.get(dedupeKey);
  if (inFlightRefresh) {
    return applyRefreshResult(
      event,
      organizationSlug,
      workspaceSlug,
      await inFlightRefresh,
    );
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

  return applyRefreshResult(
    event,
    organizationSlug,
    workspaceSlug,
    await refreshPromise,
  );
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
  organizationSlug,
  workspaceSlug,
  coreBaseUrl,
}) {
  if (!coreBaseUrl) {
    return null;
  }

  const refreshToken = readWorkspaceRefreshToken(
    event,
    organizationSlug,
    workspaceSlug,
  );
  let accessToken = readWorkspaceAccessToken(
    event,
    organizationSlug,
    workspaceSlug,
  );

  if (!refreshToken && !accessToken) {
    clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
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
      clearRetryableWorkspaceAuthFailureCount(
        event,
        organizationSlug,
        workspaceSlug,
      );
      return agent;
    } catch (error) {
      if (isTerminalAccountSessionFailure(error)) {
        throw error;
      }
      if (error?.status !== 401) {
        throw error;
      }
      if (!refreshToken) {
        clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
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
      organizationSlug,
      workspaceSlug,
      coreBaseUrl,
    });
    accessToken = readWorkspaceAccessToken(
      event,
      organizationSlug,
      workspaceSlug,
    );
    if (!accessToken) {
      return null;
    }
    const agent = await fetchCurrentAgent(accessToken);
    clearRetryableWorkspaceAuthFailureCount(
      event,
      organizationSlug,
      workspaceSlug,
    );
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
          organizationSlug,
          workspaceSlug,
        )
      ) {
        clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
        return null;
      }
      throw createRetryableAuthSessionError(error);
    }
    if (error?.status === 401) {
      clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
      return null;
    }
    throw error;
  }
}

export async function handleWorkspaceAuthVerifyResponse({
  event,
  organizationSlug,
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
    writeWorkspaceRefreshToken(
      event,
      organizationSlug,
      workspaceSlug,
      refreshToken,
    );
  }
  if (accessToken) {
    writeWorkspaceAccessToken(
      event,
      organizationSlug,
      workspaceSlug,
      accessToken,
    );
  }
  clearRetryableWorkspaceAuthFailureCount(
    event,
    organizationSlug,
    workspaceSlug,
  );

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
  organizationSlug,
  workspaceSlug,
  coreBaseUrl,
  pathname,
}) {
  const targetUrl = coreEndpointURL(
    coreBaseUrlForNodeFetch(coreBaseUrl),
    pathname,
  );
  const requestInit = buildProxyRequestInit(event);
  requestInit.headers.delete("cookie");
  requestInit.headers.delete("authorization");
  requestInit.headers.delete(WORKSPACE_HEADER);

  const upstreamResponse = await fetch(targetUrl, requestInit);
  return handleWorkspaceAuthVerifyResponse({
    event,
    organizationSlug,
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
