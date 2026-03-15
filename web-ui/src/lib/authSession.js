import { get, writable } from "svelte/store";

import { clearSelectedActor } from "./actorSession.js";
import { normalizeBaseUrl } from "./config.js";
import { getCurrentProjectSlug, currentProjectSlug } from "./projectContext.js";
import {
  DEFAULT_PROJECT_SLUG,
  appPath,
  buildProjectStorageKey,
  projectPath,
} from "./projectPaths.js";

export const REFRESH_TOKEN_STORAGE_KEY = "oar_ui_refresh_token";

export const authSessionReady = writable(false);
export const authenticatedAgent = writable(null);

const browser = typeof window !== "undefined";

const authStateByProject = new Map();

function createEmptyAuthState() {
  return {
    ready: false,
    accessToken: "",
    authenticatedAgent: null,
    refreshPromise: undefined,
  };
}

function ensureAuthState(projectSlug = getCurrentProjectSlug()) {
  const slug = String(projectSlug ?? "").trim();
  if (!authStateByProject.has(slug)) {
    authStateByProject.set(slug, createEmptyAuthState());
  }

  return authStateByProject.get(slug);
}

function syncCurrentAuthStores(projectSlug = getCurrentProjectSlug()) {
  const state = ensureAuthState(projectSlug);
  authSessionReady.set(state.ready);
  authenticatedAgent.set(state.authenticatedAgent);
  return state;
}

currentProjectSlug.subscribe((projectSlug) => {
  syncCurrentAuthStores(projectSlug);
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
  { fetchFn, method = "GET", body, token, baseUrl } = {},
) {
  const response = await resolveFetch(fetchFn)(buildUrl(pathname, baseUrl), {
    method,
    headers: {
      accept: "application/json",
      ...(body ? { "content-type": "application/json" } : {}),
      ...(token ? { authorization: `Bearer ${token}` } : {}),
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

export function refreshTokenStorageKey(projectSlug = getCurrentProjectSlug()) {
  return buildProjectStorageKey(REFRESH_TOKEN_STORAGE_KEY, projectSlug);
}

export function getAccessToken(projectSlug = getCurrentProjectSlug()) {
  return ensureAuthState(projectSlug).accessToken;
}

export function loadStoredRefreshToken(
  storage = sessionStorage,
  projectSlug = getCurrentProjectSlug(),
) {
  const scopedRefreshToken = storage.getItem(
    refreshTokenStorageKey(projectSlug),
  );
  if (scopedRefreshToken) {
    return scopedRefreshToken;
  }

  const normalizedProjectSlug = String(projectSlug ?? "").trim();
  if (
    !normalizedProjectSlug ||
    normalizedProjectSlug === DEFAULT_PROJECT_SLUG
  ) {
    return storage.getItem(REFRESH_TOKEN_STORAGE_KEY) ?? "";
  }

  return "";
}

export function saveRefreshToken(
  refreshToken,
  storage = sessionStorage,
  projectSlug = getCurrentProjectSlug(),
) {
  const normalized = String(refreshToken ?? "").trim();
  const storageKey = refreshTokenStorageKey(projectSlug);
  if (!normalized) {
    storage.removeItem(storageKey);
    storage.removeItem(REFRESH_TOKEN_STORAGE_KEY);
    return "";
  }

  storage.setItem(storageKey, normalized);
  return normalized;
}

export function clearAuthSession(
  storage = browser ? sessionStorage : undefined,
  projectSlug = getCurrentProjectSlug(),
  options = {},
) {
  const clearActor = Boolean(options.clearActor);
  const state = ensureAuthState(projectSlug);
  state.accessToken = "";
  state.authenticatedAgent = null;
  state.refreshPromise = undefined;
  state.ready = true;
  if (storage) {
    storage.removeItem(refreshTokenStorageKey(projectSlug));
    storage.removeItem(REFRESH_TOKEN_STORAGE_KEY);
  }
  if (browser && clearActor) {
    clearSelectedActor(localStorage, projectSlug);
  }
  syncCurrentAuthStores(projectSlug);
}

export function completeAuthSession(
  agent,
  tokens,
  storage = sessionStorage,
  projectSlug = getCurrentProjectSlug(),
) {
  const state = ensureAuthState(projectSlug);
  state.accessToken = String(tokens?.access_token ?? "").trim();
  saveRefreshToken(tokens?.refresh_token, storage, projectSlug);
  state.authenticatedAgent = agent ?? null;
  state.ready = true;
  syncCurrentAuthStores(projectSlug);
  return {
    agent: agent ?? null,
    tokens,
  };
}

export function getAuthenticatedAgent(projectSlug = getCurrentProjectSlug()) {
  if (projectSlug && projectSlug !== getCurrentProjectSlug()) {
    return ensureAuthState(projectSlug).authenticatedAgent;
  }

  return get(authenticatedAgent);
}

export function getAuthenticatedActorId(projectSlug = getCurrentProjectSlug()) {
  return getAuthenticatedAgent(projectSlug)?.actor_id ?? "";
}

export function isAuthenticated(projectSlug = getCurrentProjectSlug()) {
  return Boolean(getAuthenticatedAgent(projectSlug)?.agent_id);
}

export async function refreshAuthSession({
  fetchFn,
  storage = sessionStorage,
  baseUrl = "",
  projectSlug = getCurrentProjectSlug(),
  redirectOnFailure = false,
} = {}) {
  const state = ensureAuthState(projectSlug);
  const refreshToken = loadStoredRefreshToken(storage, projectSlug);
  if (!refreshToken) {
    clearAuthSession(storage, projectSlug);
    return null;
  }

  if (!state.refreshPromise) {
    state.refreshPromise = (async () => {
      const tokenResponse = await requestJSON("/auth/token", {
        fetchFn,
        baseUrl,
        method: "POST",
        body: {
          grant_type: "refresh_token",
          refresh_token: refreshToken,
        },
      });
      const nextTokens = tokenResponse.tokens ?? {};
      state.accessToken = String(nextTokens.access_token ?? "").trim();
      saveRefreshToken(nextTokens.refresh_token, storage, projectSlug);

      const meResponse = await requestJSON("/agents/me", {
        fetchFn,
        baseUrl,
        token: nextTokens.access_token,
      });

      state.authenticatedAgent = meResponse.agent ?? null;
      state.ready = true;
      if (projectSlug === getCurrentProjectSlug()) {
        syncCurrentAuthStores(projectSlug);
      }
      return {
        agent: meResponse.agent ?? null,
        tokens: nextTokens,
      };
    })()
      .catch((error) => {
        clearAuthSession(storage, projectSlug);
        if (redirectOnFailure && browser) {
          window.location.assign(projectPath(projectSlug, "/login"));
        }
        throw error;
      })
      .finally(() => {
        state.refreshPromise = undefined;
      });
  }

  return state.refreshPromise;
}

export async function initializeAuthSession({
  fetchFn,
  storage = browser ? sessionStorage : undefined,
  baseUrl = "",
  projectSlug = getCurrentProjectSlug(),
} = {}) {
  const state = ensureAuthState(projectSlug);
  if (!browser || !storage) {
    state.ready = true;
    syncCurrentAuthStores(projectSlug);
    return null;
  }

  state.ready = false;
  syncCurrentAuthStores(projectSlug);

  try {
    const result = await refreshAuthSession({
      fetchFn,
      storage,
      baseUrl,
      projectSlug,
      redirectOnFailure: false,
    });
    state.ready = true;
    syncCurrentAuthStores(projectSlug);
    return result;
  } catch {
    state.ready = true;
    syncCurrentAuthStores(projectSlug);
    return null;
  }
}

export function createAuthTokenProvider(fetchFn, options = {}) {
  const projectSlugProvider =
    options.projectSlugProvider ?? (() => getCurrentProjectSlug());
  const baseUrl = options.baseUrl ?? "";

  return {
    getAccessToken() {
      return getAccessToken(projectSlugProvider());
    },
    hasRefreshToken(storage = sessionStorage) {
      return Boolean(loadStoredRefreshToken(storage, projectSlugProvider()));
    },
    async refreshAccessToken() {
      const result = await refreshAuthSession({
        fetchFn,
        baseUrl,
        projectSlug: projectSlugProvider(),
        redirectOnFailure: true,
      });
      return result?.tokens?.access_token ?? "";
    },
    async handleRefreshFailure() {
      const projectSlug = projectSlugProvider();
      clearAuthSession(undefined, projectSlug);
      if (browser) {
        window.location.assign(projectPath(projectSlug, "/login"));
      }
    },
  };
}
