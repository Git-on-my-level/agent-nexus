/**
 * Client workspace auth state machine (per organization and workspace):
 *
 * - **authSessionReady** / internal `ready`: `/auth/session` hydration finished
 *   (success or handled failure).
 * - **authenticatedAgent**: current agent row or null.
 * - **sessionEndedByAccountStatus**: terminal account-status revocation; set when
 *   `/auth/session` returns **401** with
 *   `error.code === session_ended_by_account_status` (see
 *   {@link AuthErrorCode.SESSION_ENDED_BY_ACCOUNT_STATUS}).
 *
 * **Single-flight:** concurrent {@link initializeAuthSession} calls share one
 * in-flight promise. The shell should pass `authDriver: "layout"` so devtools
 * can spot a second driver (e.g. login page) fighting the layout.
 *
 * @see `$lib/server/authSession.js` for refresh dedup and cookie naming.
 */

import { get, writable } from "svelte/store";

import { clearWorkCache } from "./workCache.js";
import { clearWorkspaceViews } from "./workspaceViewCache.js";

import { AuthErrorCode } from "./authErrorCodes.js";
import { clearSelectedActor } from "./actorSession.js";
import { buildCoreRequestContextHeaders } from "./coreClientRequestHeaders.js";
import { normalizeBaseUrl } from "./config.js";
import {
  getCurrentOrganizationSlug,
  getCurrentWorkspaceSlug,
  currentWorkspaceSlug,
  currentOrganizationSlug,
} from "./workspaceContext.js";
import { APP_BASE_PATH, WORKSPACE_HEADER, appPath } from "./workspacePaths.js";

export const authSessionReady = writable(false);
export const authenticatedAgent = writable(null);
/** True when account status ended the session ({@link AuthErrorCode.SESSION_ENDED_BY_ACCOUNT_STATUS}). */
export const sessionEndedByAccountStatus = writable(false);

/** @type {Map<string, string>} */
const authDriverByWorkspace = new Map();

const browser = typeof window !== "undefined";
const AUTH_SESSION_RETRYABLE_ERROR_CODE = AuthErrorCode.AUTH_SESSION_RETRYABLE;
const AUTH_SESSION_INIT_MAX_ATTEMPTS = 2;
const AUTH_SESSION_INIT_RETRY_DELAY_MS = 150;

const authStateByWorkspace = new Map();

function createEmptyAuthState() {
  return {
    ready: false,
    accessToken: "",
    authenticatedAgent: null,
    /**
     * Promise of the currently in-flight initializeAuthSession call for this
     * workspace, or null when nothing is in flight. Used to dedupe concurrent
     * callers so that a reactive effect cannot spawn N parallel `/auth/session`
     * requests while one is already running.
     */
    initInflight: null,
    generation: 0,
  };
}

function ensureAuthState(
  workspaceSlug = getCurrentWorkspaceSlug(),
  organizationSlug = getCurrentOrganizationSlug(),
) {
  const slug = `${organizationSlug}/${String(workspaceSlug ?? "").trim()}`;
  if (!authStateByWorkspace.has(slug)) {
    authStateByWorkspace.set(slug, createEmptyAuthState());
  }

  return authStateByWorkspace.get(slug);
}

function syncCurrentAuthStores(
  workspaceSlug = getCurrentWorkspaceSlug(),
  organizationSlug = getCurrentOrganizationSlug(),
) {
  const state = ensureAuthState(workspaceSlug, organizationSlug);
  if (
    getCurrentWorkspaceSlug() &&
    (workspaceSlug !== getCurrentWorkspaceSlug() ||
      organizationSlug !== getCurrentOrganizationSlug())
  )
    return state;
  authSessionReady.set(state.ready);
  authenticatedAgent.set(state.authenticatedAgent);
  return state;
}

currentWorkspaceSlug.subscribe(() => syncCurrentAuthStores());
currentOrganizationSlug.subscribe(() => syncCurrentAuthStores());

function resolveFetch(fetchFn) {
  if (typeof fetchFn === "function") {
    return fetchFn;
  }

  return globalThis.fetch.bind(globalThis);
}

function buildUrl(pathname, baseUrl = "") {
  const resolvedBaseUrl = normalizeBaseUrl(baseUrl);
  if (!resolvedBaseUrl) {
    return appPath(pathname);
  }

  return new URL(pathname, `${resolvedBaseUrl}/`).toString();
}

function createErrorFromResponse(status, details) {
  const message =
    details?.error?.message || details?.message || `request failed (${status})`;
  const error = new Error(message);
  error.status = status;
  error.details = details;
  return error;
}

function applySessionEndedByAccountStatus(
  status,
  payload,
  workspaceSlug,
  organizationSlug,
) {
  if (
    status !== 401 ||
    payload?.error?.code !== AuthErrorCode.SESSION_ENDED_BY_ACCOUNT_STATUS
  ) {
    return false;
  }
  sessionEndedByAccountStatus.set(true);
  const slug = String(workspaceSlug ?? "").trim() || getCurrentWorkspaceSlug();
  clearAuthSession(slug, { organizationSlug });
  return true;
}

function shouldPreserveAuthenticatedAgentOnInitFailure(error) {
  if (!error || typeof error !== "object") {
    return true;
  }

  if (isRetryableAuthSessionFailure(error)) {
    return false;
  }

  const status = Number(error.status);
  return !Number.isFinite(status) || status >= 500;
}

function isRetryableAuthSessionFailure(error) {
  return (
    Number(error?.status) === 503 &&
    error?.details?.error?.code === AUTH_SESSION_RETRYABLE_ERROR_CODE
  );
}

function wait(ms) {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

async function requestJSON(
  pathname,
  { fetchFn, method = "GET", body, baseUrl, headers } = {},
) {
  const mergedHeaders = {
    ...(browser
      ? buildCoreRequestContextHeaders({
          storeOrg: getCurrentOrganizationSlug(),
          storeWorkspace: getCurrentWorkspaceSlug(),
          pathname: globalThis.location?.pathname ?? "/",
          basePath: APP_BASE_PATH,
        })
      : {}),
    accept: "application/json",
    ...(body ? { "content-type": "application/json" } : {}),
    ...(headers ?? {}),
  };
  const response = await resolveFetch(fetchFn)(buildUrl(pathname, baseUrl), {
    method,
    signal: AbortSignal.timeout(15_000),
    headers: mergedHeaders,
    body: body ? JSON.stringify(body) : undefined,
  });

  const rawText = await response.text();
  let payload = {};
  if (rawText) {
    try {
      payload = JSON.parse(rawText);
    } catch {
      payload = { message: rawText };
    }
  }
  if (!response.ok) {
    throw createErrorFromResponse(response.status, payload);
  }

  return payload;
}

export function getAccessToken(workspaceSlug = getCurrentWorkspaceSlug()) {
  return ensureAuthState(workspaceSlug).accessToken;
}

export function getAuthenticatedAgent(
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  if (workspaceSlug && workspaceSlug !== getCurrentWorkspaceSlug()) {
    return ensureAuthState(workspaceSlug).authenticatedAgent;
  }

  return get(authenticatedAgent);
}

export function getAuthenticatedActorId(
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  return getAuthenticatedAgent(workspaceSlug)?.actor_id ?? "";
}

export function isAuthenticated(workspaceSlug = getCurrentWorkspaceSlug()) {
  return Boolean(getAuthenticatedAgent(workspaceSlug)?.agent_id);
}

/** Human principals for workspace auth (passkey, hosted account directory, external grant). Matches core `human_only` routes. */
export function isHumanWorkspacePrincipal(agent) {
  if (!agent || typeof agent !== "object") {
    return false;
  }
  if (agent.principal_kind === "human") {
    return true;
  }
  const method = String(agent.auth_method ?? "")
    .trim()
    .toLowerCase();
  return (
    method === "passkey" ||
    method === "control_plane" || // core auth_method sentinel (not user-facing)
    method === "external_grant"
  );
}

export function completeAuthSession(
  agent,
  workspaceSlug = getCurrentWorkspaceSlug(),
  { organizationSlug = getCurrentOrganizationSlug() } = {},
) {
  const state = ensureAuthState(workspaceSlug, organizationSlug);
  state.generation += 1;
  state.initInflight = null;
  state.accessToken = "";
  if (state.authenticatedAgent && !sameAgent(state.authenticatedAgent, agent)) {
    clearWorkspaceViews();
    clearWorkCache();
  }
  state.authenticatedAgent = agent ?? null;
  state.ready = true;
  syncCurrentAuthStores(workspaceSlug, organizationSlug);
  return {
    agent: agent ?? null,
  };
}

export function clearAuthSession(
  workspaceSlug = getCurrentWorkspaceSlug(),
  options = {},
) {
  // Both display caches go with the session: a card's title and body are as
  // much of the workspace as a page snapshot is, and neither should outlive
  // the identity that was allowed to read it.
  clearWorkspaceViews();
  clearWorkCache();
  const clearActor = Boolean(options.clearActor);
  const organizationSlug =
    options.organizationSlug ?? getCurrentOrganizationSlug();
  const state = ensureAuthState(workspaceSlug, organizationSlug);
  state.accessToken = "";
  state.authenticatedAgent = null;
  state.ready = true;
  state.initInflight = null;
  state.generation += 1;
  if (browser && clearActor) {
    clearSelectedActor(localStorage, workspaceSlug);
  }
  syncCurrentAuthStores(workspaceSlug, organizationSlug);
}

export async function initializeAuthSession({
  fetchFn,
  baseUrl = "",
  workspaceSlug = getCurrentWorkspaceSlug(),
  organizationSlug = getCurrentOrganizationSlug(),
  /** @type {"layout" | "login" | string | undefined} */
  authDriver,
} = {}) {
  const slug = String(workspaceSlug ?? "").trim();
  if (authDriver) {
    const prev = authDriverByWorkspace.get(`${organizationSlug}/${slug}`);
    if (prev && prev !== authDriver && import.meta.env.DEV) {
      console.warn(
        `[auth] initializeAuthSession: conflicting authDriver for workspace "${slug}" (${prev} vs ${authDriver})`,
      );
    }
    authDriverByWorkspace.set(`${organizationSlug}/${slug}`, authDriver);
  }

  const state = ensureAuthState(workspaceSlug, organizationSlug);

  // Single-flight: if a previous call is still pending for this workspace,
  // return that same promise. Prevents reactive effects (e.g. those watching
  // `authSessionReady`) from spawning a flood of parallel `/auth/session`
  // requests when stores update mid-flight. Without this guard, a Svelte
  // `$effect` that depends on `authSessionReady` and calls
  // `initializeAuthSession` will recurse — `ready` flips, the effect re-runs,
  // it issues another fetch, repeat — until the browser hits
  // ERR_INSUFFICIENT_RESOURCES.
  if (state.initInflight) {
    return state.initInflight;
  }

  const promise = runInitializeAuthSession({
    fetchFn,
    baseUrl,
    workspaceSlug,
    organizationSlug,
    state,
  }).finally(() => {
    if (state.initInflight === promise) {
      state.initInflight = null;
    }
  });

  state.initInflight = promise;
  return promise;
}

async function runInitializeAuthSession({
  fetchFn,
  baseUrl,
  workspaceSlug,
  organizationSlug,
  state,
}) {
  const generation = state.generation;
  const previousAgent = state.authenticatedAgent;
  // Track whether this is the first time we're hydrating this workspace.
  // On first init, flip `ready` to false so consumers can show a loading
  // state. On subsequent refreshes, leave `ready` untouched — flipping it
  // would invalidate every `$derived(... && $authSessionReady)` computation
  // and re-fire any `$effect` that depends on them, which is what creates
  // the loop. Refreshes update the agent only when the fetch resolves.
  const isInitialHydration = !state.ready;

  if (!browser && typeof fetchFn !== "function") {
    state.ready = true;
    syncCurrentAuthStores(workspaceSlug, organizationSlug);
    return null;
  }

  if (isInitialHydration) {
    state.ready = false;
    syncCurrentAuthStores(workspaceSlug, organizationSlug);
  }

  for (
    let attempt = 0;
    attempt < AUTH_SESSION_INIT_MAX_ATTEMPTS;
    attempt += 1
  ) {
    try {
      const result = await requestJSON("/auth/session", {
        fetchFn,
        baseUrl,
        workspaceSlug,
        headers: {
          [WORKSPACE_HEADER]: workspaceSlug,
          "x-anx-organization-slug": organizationSlug,
        },
      });
      if (generation !== state.generation) return null;
      const nextAgent = result.agent ?? null;
      const agentChanged = !sameAgent(previousAgent, nextAgent);
      if (agentChanged && previousAgent) {
        clearWorkspaceViews();
        clearWorkCache();
      }
      state.authenticatedAgent = nextAgent;
      state.ready = true;
      if (isInitialHydration || agentChanged) {
        syncCurrentAuthStores(workspaceSlug, organizationSlug);
      }
      return nextAgent;
    } catch (error) {
      if (generation !== state.generation) return null;
      applySessionEndedByAccountStatus(
        error.status,
        error.details,
        workspaceSlug,
        organizationSlug,
      );
      if (
        isRetryableAuthSessionFailure(error) &&
        attempt < AUTH_SESSION_INIT_MAX_ATTEMPTS - 1
      ) {
        await wait(AUTH_SESSION_INIT_RETRY_DELAY_MS);
        continue;
      }

      const nextAgent = shouldPreserveAuthenticatedAgentOnInitFailure(error)
        ? previousAgent
        : null;
      const agentChanged = !sameAgent(previousAgent, nextAgent);
      if (agentChanged && previousAgent) {
        clearWorkspaceViews();
        clearWorkCache();
      }
      state.authenticatedAgent = nextAgent;
      state.ready = true;
      if (isInitialHydration || agentChanged) {
        syncCurrentAuthStores(workspaceSlug, organizationSlug);
      }
      return state.authenticatedAgent;
    }
  }
}

function sameAgent(a, b) {
  if (a === b) return true;
  if (!a || !b) return false;
  return (
    a.agent_id === b.agent_id &&
    a.actor_id === b.actor_id &&
    (a.username ?? "") === (b.username ?? "") &&
    (a.auth_method ?? "") === (b.auth_method ?? "") &&
    (a.principal_kind ?? "") === (b.principal_kind ?? "")
  );
}

export async function logoutAuthSession({
  fetchFn,
  baseUrl = "",
  workspaceSlug = getCurrentWorkspaceSlug(),
  clearActor = false,
} = {}) {
  const organizationSlug = getCurrentOrganizationSlug();
  const pending = ensureAuthState(workspaceSlug, organizationSlug).initInflight;
  clearAuthSession(workspaceSlug, { clearActor, organizationSlug });
  // Let earlier Set-Cookie responses settle before the DELETE clears cookies.
  if (pending) await pending.catch(() => {});
  if (browser || typeof fetchFn === "function") {
    try {
      await requestJSON("/auth/session", {
        fetchFn,
        baseUrl,
        workspaceSlug,
        method: "DELETE",
        headers: {
          [WORKSPACE_HEADER]: workspaceSlug,
          "x-anx-organization-slug": organizationSlug,
        },
      });
    } catch {
      // Fall through to local cleanup. Logout should be best-effort.
    }
  }
}

export function createAuthTokenProvider() {
  return {
    getAccessToken() {
      return "";
    },
    hasRefreshToken() {
      return false;
    },
    async refreshAccessToken() {
      return "";
    },
    async handleRefreshFailure() {},
  };
}

/** Revalidate visited sessions without blocking navigation. Tokens never enter JS. */
export function startWorkspaceSessionMaintenance({ intervalMs = 60_000 } = {}) {
  const refresh = () => {
    if (globalThis.document?.visibilityState === "hidden") return;
    for (const [key, state] of authStateByWorkspace) {
      if (!state.authenticatedAgent) continue;
      const [organizationSlug, workspaceSlug] = key.split("/");
      void initializeAuthSession({
        organizationSlug,
        workspaceSlug,
        authDriver: "layout",
      });
    }
  };
  const timer = setInterval(refresh, intervalMs);
  globalThis.document?.addEventListener("visibilitychange", refresh);
  return () => {
    clearInterval(timer);
    globalThis.document?.removeEventListener("visibilitychange", refresh);
  };
}

// Cross-tab notifications carry no credentials. Server cookie scoping remains
// authoritative even if a tab is asleep or misses a notification.
let sessionEpoch = 0;
export const getSessionEpoch = () => sessionEpoch;
let accountChannel;
function invalidateWorkspaceSessions() {
  sessionEpoch += 1;
  for (const key of authStateByWorkspace.keys()) {
    const [organizationSlug, workspaceSlug] = key.split("/");
    clearAuthSession(workspaceSlug, { organizationSlug });
  }
}
if (browser && typeof globalThis.BroadcastChannel === "function") {
  accountChannel = new BroadcastChannel("anx-session-changed");
  accountChannel.onmessage = () => invalidateWorkspaceSessions();
}

/** Invalidate flights immediately; logout must not wait for old network calls. */
export async function clearAllWorkspaceAuthSessions() {
  invalidateWorkspaceSessions();
  accountChannel?.postMessage("changed");
}
