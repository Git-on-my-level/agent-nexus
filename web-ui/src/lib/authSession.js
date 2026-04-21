/**
 * Client workspace auth state machine (per workspace slug):
 *
 * - **authSessionReady** / internal `ready`: `/auth/session` hydration finished
 *   (success or handled failure).
 * - **authenticatedAgent**: current agent row or null.
 * - **sessionEndedByCp**: terminal CP revocation; set when `/auth/session`
 *   returns **401** with `error.code === session_ended_by_cp` (see
 *   {@link AuthErrorCode.SESSION_ENDED_BY_CP}).
 *
 * **Single-flight:** concurrent {@link initializeAuthSession} calls share one
 * in-flight promise. The shell should pass `authDriver: "layout"` so devtools
 * can spot a second driver (e.g. login page) fighting the layout.
 *
 * @see `$lib/server/authSession.js` for refresh dedup and cookie naming.
 */

import { get, writable } from "svelte/store";

import { AuthErrorCode } from "./authErrorCodes.js";
import { clearSelectedActor } from "./actorSession.js";
import { buildCoreRequestContextHeaders } from "./coreClientRequestHeaders.js";
import { normalizeBaseUrl } from "./config.js";
import {
  getCurrentOrganizationSlug,
  getCurrentWorkspaceSlug,
  currentWorkspaceSlug,
} from "./workspaceContext.js";
import { APP_BASE_PATH, WORKSPACE_HEADER, appPath } from "./workspacePaths.js";

export const authSessionReady = writable(false);
export const authenticatedAgent = writable(null);
/** True when CP ended the session ({@link AuthErrorCode.SESSION_ENDED_BY_CP}). */
export const sessionEndedByCp = writable(false);

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
  };
}

function ensureAuthState(workspaceSlug = getCurrentWorkspaceSlug()) {
  const slug = String(workspaceSlug ?? "").trim();
  if (!authStateByWorkspace.has(slug)) {
    authStateByWorkspace.set(slug, createEmptyAuthState());
  }

  return authStateByWorkspace.get(slug);
}

function syncCurrentAuthStores(workspaceSlug = getCurrentWorkspaceSlug()) {
  const state = ensureAuthState(workspaceSlug);
  authSessionReady.set(state.ready);
  authenticatedAgent.set(state.authenticatedAgent);
  return state;
}

currentWorkspaceSlug.subscribe((workspaceSlug) => {
  syncCurrentAuthStores(workspaceSlug);
});

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

function applySessionEndedByCp(status, payload, workspaceSlug) {
  if (status !== 401 || payload?.error?.code !== AuthErrorCode.SESSION_ENDED_BY_CP) {
    return false;
  }
  sessionEndedByCp.set(true);
  const slug = String(workspaceSlug ?? "").trim() || getCurrentWorkspaceSlug();
  clearAuthSession(slug);
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
  { fetchFn, method = "GET", body, baseUrl, headers, workspaceSlug } = {},
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
    applySessionEndedByCp(response.status, payload, workspaceSlug);
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

/** Human principals for workspace auth (passkey, control plane). Matches core `human_only` routes. */
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
    method === "control_plane" ||
    method === "external_grant"
  );
}

export function completeAuthSession(
  agent,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureAuthState(workspaceSlug);
  state.accessToken = "";
  state.authenticatedAgent = agent ?? null;
  state.ready = true;
  syncCurrentAuthStores(workspaceSlug);
  return {
    agent: agent ?? null,
  };
}

export function clearAuthSession(
  workspaceSlug = getCurrentWorkspaceSlug(),
  options = {},
) {
  const clearActor = Boolean(options.clearActor);
  const state = ensureAuthState(workspaceSlug);
  state.accessToken = "";
  state.authenticatedAgent = null;
  state.ready = true;
  state.initInflight = null;
  if (browser && clearActor) {
    clearSelectedActor(localStorage, workspaceSlug);
  }
  syncCurrentAuthStores(workspaceSlug);
}

export async function initializeAuthSession({
  fetchFn,
  baseUrl = "",
  workspaceSlug = getCurrentWorkspaceSlug(),
  /** @type {"layout" | "login" | string | undefined} */
  authDriver,
} = {}) {
  const slug = String(workspaceSlug ?? "").trim();
  if (authDriver) {
    const prev = authDriverByWorkspace.get(slug);
    if (prev && prev !== authDriver && import.meta.env.DEV) {
      console.warn(
        `[auth] initializeAuthSession: conflicting authDriver for workspace "${slug}" (${prev} vs ${authDriver})`,
      );
    }
    authDriverByWorkspace.set(slug, authDriver);
  }

  const state = ensureAuthState(workspaceSlug);

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
  state,
}) {
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
    syncCurrentAuthStores(workspaceSlug);
    return null;
  }

  if (isInitialHydration) {
    state.ready = false;
    syncCurrentAuthStores(workspaceSlug);
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
        },
      });
      const nextAgent = result.agent ?? null;
      const agentChanged = !sameAgent(previousAgent, nextAgent);
      state.authenticatedAgent = nextAgent;
      state.ready = true;
      if (isInitialHydration || agentChanged) {
        syncCurrentAuthStores(workspaceSlug);
      }
      return nextAgent;
    } catch (error) {
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
      state.authenticatedAgent = nextAgent;
      state.ready = true;
      if (isInitialHydration || agentChanged) {
        syncCurrentAuthStores(workspaceSlug);
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
  if (browser || typeof fetchFn === "function") {
    try {
      await requestJSON("/auth/session", {
        fetchFn,
        baseUrl,
        workspaceSlug,
        method: "DELETE",
        headers: {
          [WORKSPACE_HEADER]: workspaceSlug,
        },
      });
    } catch {
      // Fall through to local cleanup. Logout should be best-effort.
    }
  }

  clearAuthSession(workspaceSlug, { clearActor });
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
