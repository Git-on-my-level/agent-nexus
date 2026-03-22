import { get, writable } from "svelte/store";

import { clearSelectedActor } from "./actorSession.js";
import { normalizeBaseUrl } from "./config.js";
import {
  getCurrentWorkspaceSlug,
  currentWorkspaceSlug,
} from "./workspaceContext.js";
import { WORKSPACE_HEADER, appPath } from "./workspacePaths.js";

export const authSessionReady = writable(false);
export const authenticatedAgent = writable(null);

const browser = typeof window !== "undefined";

const authStateByWorkspace = new Map();

function createEmptyAuthState() {
  return {
    ready: false,
    accessToken: "",
    authenticatedAgent: null,
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

async function requestJSON(
  pathname,
  { fetchFn, method = "GET", body, baseUrl, headers } = {},
) {
  const response = await resolveFetch(fetchFn)(buildUrl(pathname, baseUrl), {
    method,
    headers: {
      accept: "application/json",
      ...(body ? { "content-type": "application/json" } : {}),
      ...(headers ?? {}),
    },
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
  if (browser && clearActor) {
    clearSelectedActor(localStorage, workspaceSlug);
  }
  syncCurrentAuthStores(workspaceSlug);
}

export async function initializeAuthSession({
  fetchFn,
  baseUrl = "",
  workspaceSlug = getCurrentWorkspaceSlug(),
} = {}) {
  const state = ensureAuthState(workspaceSlug);
  if (!browser && typeof fetchFn !== "function") {
    state.ready = true;
    syncCurrentAuthStores(workspaceSlug);
    return null;
  }

  state.ready = false;
  syncCurrentAuthStores(workspaceSlug);

  try {
    const result = await requestJSON("/auth/session", {
      fetchFn,
      baseUrl,
      headers: {
        [WORKSPACE_HEADER]: workspaceSlug,
      },
    });
    state.authenticatedAgent = result.agent ?? null;
    state.ready = true;
    syncCurrentAuthStores(workspaceSlug);
    return result.agent ?? null;
  } catch {
    state.ready = true;
    syncCurrentAuthStores(workspaceSlug);
    return null;
  }
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
