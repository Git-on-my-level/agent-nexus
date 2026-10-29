import { dev } from "$app/environment";
import { json } from "@sveltejs/kit";

import { AuthErrorCode } from "$lib/authErrorCodes.js";
import {
  MALFORMED_RESPONSE_CODE,
  WORKSPACE_NOT_FOUND_MESSAGE,
  WorkspaceFailureKind,
  classifyWorkspaceFailure,
} from "$lib/workspaceFailure.js";
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

function getAccessExpiryCookieName(organizationSlug, workspaceSlug) {
  return `${getAuthAccessCookieName(organizationSlug, workspaceSlug)}_expires`;
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
  if (event.locals?.sessionCookieScope) return;
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
  if (!dev) {
    return true;
  }
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
      ...(options.headers ?? {}),
      ...(options.token ? { authorization: `Bearer ${options.token}` } : {}),
    },
    body: options.body ? JSON.stringify(options.body) : undefined,
  });

  const text = await response.text();
  const payload = readJSONPayload(text);
  if (!response.ok) {
    throw createRequestError(response.status, payload);
  }
  // An unparseable success is an outage (502), not an empty answer.
  if (String(text ?? "").trim() && !isJSONText(text)) {
    throw createMalformedResponseError(pathname);
  }

  return payload;
}

function isJSONText(text) {
  try {
    JSON.parse(text);
    return true;
  } catch {
    return false;
  }
}

function createMalformedResponseError(pathname) {
  return createRequestError(502, {
    error: {
      code: MALFORMED_RESPONSE_CODE,
      message: `anx-core sent an unreadable reply for ${pathname}.`,
    },
  });
}

/**
 * `/agents/me` must name an agent. Anything else is malformed and must not
 * read as "signed out".
 */
function requireAgentPayload(payload) {
  const agent = payload?.agent;
  if (
    !agent ||
    typeof agent !== "object" ||
    typeof agent.agent_id !== "string" ||
    !agent.agent_id.trim()
  ) {
    throw createMalformedResponseError("/agents/me");
  }
  return agent;
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
      tokens.expiresAt,
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
  expiresAt = Date.now() + ACCESS_TOKEN_TTL_SECONDS * 1000,
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
  event.cookies.set(
    getAccessExpiryCookieName(organizationSlug, workspaceSlug),
    String(expiresAt),
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
    getAccessExpiryCookieName(organizationSlug, workspaceSlug),
    { path: "/" },
  );
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
  headers = {},
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
    headers,
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
        expiresAt:
          Date.now() +
          (Number(nextTokens.expires_in) || ACCESS_TOKEN_TTL_SECONDS) * 1000,
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

/**
 * When cookies hold a refresh token but the access token is absent or empty,
 * exchange the refresh once before the first proxied request to anx-core (same
 * ordering as {@link loadWorkspaceAuthenticatedAgent}).
 *
 * @param {object} params
 * @param {import('@sveltejs/kit').RequestEvent} params.event
 * @param {string} params.organizationSlug
 * @param {string} params.workspaceSlug
 * @param {string} params.coreBaseUrl Base URL for core `/auth/token` (direct core or hosted `/ws/{org}/{ws}` prefix URL)
 * @param {{ accessToken?: string, refreshToken?: string } | null} [params.session] Optional session from a prior {@link getWorkspaceAuthSession} call
 * @param {Record<string, string>} [params.headers] Extra server-side headers for the core hop
 * @returns {Promise<"rejected"|"not_found"|"unavailable"|undefined>} The
 *   settled failure when the refresh failed (see
 *   {@link settleWorkspaceSessionFailure}). Proxies still send the request
 *   without a token when the session was kept: public routes (handshake)
 *   keep working, and a 401 takes the refresh-and-retry path, which answers
 *   503 / 404 for a kept session.
 */
export async function ensureWorkspaceAccessTokenForCoreProxy({
  event,
  organizationSlug,
  workspaceSlug,
  coreBaseUrl,
  session: sessionHint,
  headers = {},
}) {
  if (!coreBaseUrl) {
    return;
  }

  const session =
    sessionHint ??
    getWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
  if (!session) {
    return;
  }
  if (String(session.accessToken ?? "").trim()) {
    return;
  }
  if (!String(session.refreshToken ?? "").trim()) {
    return;
  }

  try {
    await refreshWorkspaceAuthSession({
      event,
      organizationSlug,
      workspaceSlug,
      coreBaseUrl,
      headers,
    });
  } catch (error) {
    return settleWorkspaceSessionFailure(
      event,
      organizationSlug,
      workspaceSlug,
      error,
      { hadAccessToken: false, hadRefreshToken: true },
    );
  }
}

/**
 * Outcome of a failed workspace session read or refresh
 * (`classifyWorkspaceFailure` in `$lib/workspaceFailure.js`, shared with the
 * browser).
 * - `rejected`: the session was cleared. The server answered 401 / 403.
 * - `not_found`: the session was kept. The workspace itself is missing (404);
 *   callers show a definitive "not found" state, and no other workspace's
 *   session is touched.
 * - `unavailable`: the session was kept. Nothing authoritative was learned
 *   (outage, timeout, network error, malformed reply, or a refresh-rotation
 *   race), so the caller reports a retryable "workspace unavailable" state.
 */
export const WorkspaceSessionFailure = WorkspaceFailureKind;

/**
 * The one rule for every workspace session path: only an authoritative
 * rejection (HTTP 401 or 403, e.g. `invalid_token`,
 * `session_ended_by_account_status`, `agent_revoked`) means the session is
 * gone; a 404 means the workspace is; everything else (5xx including
 * `auth_session_retryable`, other statuses, network errors, timeouts,
 * aborts, malformed or shape-invalid successes) is an outage, and an outage
 * must never sign the viewer out.
 */
export const classifyWorkspaceSessionFailure = classifyWorkspaceFailure;

/**
 * Apply {@link classifyWorkspaceSessionFailure} to a failed refresh: clear the
 * session on an authoritative rejection, keep it otherwise (not found or
 * unavailable). A 401
 * `invalid_token` while the caller still held a token can be a concurrent
 * refresh rotation, so it keeps the session once and clears it on the second
 * consecutive occurrence (counted in a short-lived httpOnly cookie).
 *
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {string} organizationSlug
 * @param {string} workspaceSlug
 * @param {unknown} error
 * @param {{ hadAccessToken?: boolean, hadRefreshToken?: boolean }} [options]
 * @returns {"rejected"|"not_found"|"unavailable"}
 */
export function settleWorkspaceSessionFailure(
  event,
  organizationSlug,
  workspaceSlug,
  error,
  { hadAccessToken = false, hadRefreshToken = false } = {},
) {
  const kind = classifyWorkspaceSessionFailure(error);
  if (kind !== WorkspaceSessionFailure.REJECTED) {
    return kind;
  }
  if (
    isRetryableWorkspaceRefreshFailure(error, {
      hadAccessToken,
      hadRefreshToken,
    }) &&
    !shouldClearWorkspaceAuthSessionAfterRetryableFailure(
      event,
      organizationSlug,
      workspaceSlug,
    )
  ) {
    return WorkspaceSessionFailure.UNAVAILABLE;
  }
  clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
  return WorkspaceSessionFailure.REJECTED;
}

/**
 * Proxy reply when the session was kept but no usable access token exists
 * right now. A 401 here would read as "signed out" in the browser while the
 * cookies are still valid, so the proxy answers 503 `auth_session_retryable`.
 *
 * @param {Record<string, string>} [extraHeaders]
 */
export function workspaceSessionUnavailableResponse(extraHeaders = {}) {
  return new Response(
    JSON.stringify({
      error: {
        code: RETRYABLE_AUTH_SESSION_ERROR_CODE,
        message:
          "This workspace is temporarily unreachable. You are still signed in. Try again in a moment.",
      },
    }),
    {
      status: 503,
      headers: {
        "content-type": "application/json",
        "cache-control": "no-store",
        "retry-after": "2",
        ...extraHeaders,
      },
    },
  );
}

/**
 * Proxy reply when the workspace itself is missing (404 on refresh). The
 * session is kept; the browser shows its definitive "not found" state.
 *
 * @param {Record<string, string>} [extraHeaders]
 */
export function workspaceSessionNotFoundResponse(extraHeaders = {}) {
  return new Response(
    JSON.stringify({
      error: {
        code: AuthErrorCode.WORKSPACE_NOT_FOUND,
        message: WORKSPACE_NOT_FOUND_MESSAGE,
      },
    }),
    {
      status: 404,
      headers: {
        "content-type": "application/json",
        "cache-control": "no-store",
        ...extraHeaders,
      },
    },
  );
}

/**
 * The proxy reply for a kept session ({@link settleWorkspaceSessionFailure}
 * returned `not_found` or `unavailable`), or `null` when it was rejected and
 * the upstream 401 should pass through.
 *
 * @param {"rejected"|"not_found"|"unavailable"|undefined} outcome
 * @param {Record<string, string>} [extraHeaders]
 */
export function workspaceSessionFailureResponse(outcome, extraHeaders = {}) {
  if (outcome === WorkspaceSessionFailure.NOT_FOUND)
    return workspaceSessionNotFoundResponse(extraHeaders);
  if (outcome === WorkspaceSessionFailure.UNAVAILABLE)
    return workspaceSessionUnavailableResponse(extraHeaders);
  return null;
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
  headers = {},
  readOnly = false,
}) {
  if (!coreBaseUrl) {
    return null;
  }

  let accessToken = readWorkspaceAccessToken(
    event,
    organizationSlug,
    workspaceSlug,
  );

  // Speculative route loads must never rotate sessions or mutate cookies.
  if (readOnly) {
    if (!accessToken) return null;
    try {
      const payload = await requestCoreJSON(coreBaseUrl, "/agents/me", {
        token: accessToken,
        headers,
      });
      return requireAgentPayload(payload);
    } catch (failure) {
      // Signed out only on an authoritative rejection. An outage rejects so
      // the caller can report the deferred read instead of "no session".
      if (
        classifyWorkspaceSessionFailure(failure) ===
        WorkspaceSessionFailure.REJECTED
      )
        return null;
      throw failure;
    }
  }

  const refreshToken = readWorkspaceRefreshToken(
    event,
    organizationSlug,
    workspaceSlug,
  );

  // Native legacy migration may have populated access while reading refresh.
  accessToken = readWorkspaceAccessToken(
    event,
    organizationSlug,
    workspaceSlug,
  );

  if (!refreshToken && !accessToken) {
    clearWorkspaceAuthSession(event, organizationSlug, workspaceSlug);
    return null;
  }

  // Core access tokens are opaque. This httpOnly timestamp is only a refresh
  // scheduling hint; /agents/me remains the authority for token validity.
  const expiresAt = Number(
    event.cookies.get(
      getAccessExpiryCookieName(organizationSlug, workspaceSlug),
    ),
  );
  if (
    accessToken &&
    refreshToken &&
    expiresAt > 0 &&
    expiresAt <= Date.now() + 120_000
  )
    accessToken = "";

  async function fetchCurrentAgent(token) {
    const agentResponse = await requestCoreJSON(coreBaseUrl, "/agents/me", {
      token,
      headers,
    });
    return requireAgentPayload(agentResponse);
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
      headers,
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
    const outcome = settleWorkspaceSessionFailure(
      event,
      organizationSlug,
      workspaceSlug,
      error,
      {
        hadAccessToken: Boolean(accessToken),
        hadRefreshToken: Boolean(refreshToken),
      },
    );
    if (outcome === WorkspaceSessionFailure.REJECTED) {
      return null;
    }
    // Kept: an outage keeps its own status (or none, for network errors); a
    // kept authoritative-looking 401 (refresh-rotation race) becomes 503.
    throw classifyWorkspaceSessionFailure(error) ===
      WorkspaceSessionFailure.REJECTED
      ? createRetryableAuthSessionError(error)
      : error;
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
