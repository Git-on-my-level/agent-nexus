import { get, writable } from "svelte/store";

import { clearSelectedActor } from "./actorSession.js";
import { normalizeBaseUrl } from "./config.js";
import {
  getCurrentWorkspaceSlug,
  currentWorkspaceSlug,
} from "./workspaceContext.js";
import {
  DEFAULT_WORKSPACE_SLUG,
  appPath,
  buildWorkspaceStorageKey,
  workspacePath,
} from "./workspacePaths.js";
import { buildLegacyProjectStorageKey } from "./compat/workspaceCompat.js";

export const REFRESH_TOKEN_STORAGE_KEY = "oar_ui_refresh_token";
export const LEGACY_REFRESH_TOKEN_KEY = "oar_ui_refresh_token";

export const authSessionReady = writable(false);
export const authenticatedAgent = writable(null);

const browser = typeof window !== "undefined";

const authStateByWorkspace = new Map();

function createEmptyAuthState() {
  return {
    ready: false,
    accessToken: "",
    authenticatedAgent: null,
    refreshPromise: undefined,
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

function migrateProjectStorageKey(storage, workspaceSlug) {
  const oldKey = buildLegacyProjectStorageKey(
    REFRESH_TOKEN_STORAGE_KEY,
    workspaceSlug,
  );
  const newKey = buildWorkspaceStorageKey(
    REFRESH_TOKEN_STORAGE_KEY,
    workspaceSlug,
  );

  if (oldKey === newKey) return;

  const oldValue = storage.getItem(oldKey);
  if (oldValue && !storage.getItem(newKey)) {
    storage.setItem(newKey, oldValue);
  }
}

export function refreshTokenStorageKey(
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  return buildWorkspaceStorageKey(REFRESH_TOKEN_STORAGE_KEY, workspaceSlug);
}

export function getAccessToken(workspaceSlug = getCurrentWorkspaceSlug()) {
  return ensureAuthState(workspaceSlug).accessToken;
}

export function loadStoredRefreshToken(
  storage = sessionStorage,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  migrateProjectStorageKey(storage, workspaceSlug);

  const scopedRefreshToken = storage.getItem(
    refreshTokenStorageKey(workspaceSlug),
  );
  if (scopedRefreshToken) {
    return scopedRefreshToken;
  }

  const normalizedWorkspaceSlug = String(workspaceSlug ?? "").trim();
  if (
    !normalizedWorkspaceSlug ||
    normalizedWorkspaceSlug === DEFAULT_WORKSPACE_SLUG
  ) {
    return storage.getItem(REFRESH_TOKEN_STORAGE_KEY) ?? "";
  }

  return "";
}

export function saveRefreshToken(
  refreshToken,
  storage = sessionStorage,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const normalized = String(refreshToken ?? "").trim();
  const storageKey = refreshTokenStorageKey(workspaceSlug);
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
  workspaceSlug = getCurrentWorkspaceSlug(),
  options = {},
) {
  const clearActor = Boolean(options.clearActor);
  const state = ensureAuthState(workspaceSlug);
  state.accessToken = "";
  state.authenticatedAgent = null;
  state.refreshPromise = undefined;
  state.ready = true;
  if (storage) {
    storage.removeItem(refreshTokenStorageKey(workspaceSlug));
    storage.removeItem(REFRESH_TOKEN_STORAGE_KEY);
  }
  if (browser && clearActor) {
    clearSelectedActor(localStorage, workspaceSlug);
  }
  syncCurrentAuthStores(workspaceSlug);
}

export function completeAuthSession(
  agent,
  tokens,
  storage = sessionStorage,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureAuthState(workspaceSlug);
  state.accessToken = String(tokens?.access_token ?? "").trim();
  saveRefreshToken(tokens?.refresh_token, storage, workspaceSlug);
  state.authenticatedAgent = agent ?? null;
  state.ready = true;
  syncCurrentAuthStores(workspaceSlug);
  return {
    agent: agent ?? null,
    tokens,
  };
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

export async function refreshAuthSession({
  fetchFn,
  storage = sessionStorage,
  baseUrl = "",
  workspaceSlug = getCurrentWorkspaceSlug(),
  redirectOnFailure = false,
} = {}) {
  const state = ensureAuthState(workspaceSlug);
  const refreshToken = loadStoredRefreshToken(storage, workspaceSlug);
  if (!refreshToken) {
    clearAuthSession(storage, workspaceSlug);
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
      saveRefreshToken(nextTokens.refresh_token, storage, workspaceSlug);

      const meResponse = await requestJSON("/agents/me", {
        fetchFn,
        baseUrl,
        token: nextTokens.access_token,
      });

      state.authenticatedAgent = meResponse.agent ?? null;
      state.ready = true;
      if (workspaceSlug === getCurrentWorkspaceSlug()) {
        syncCurrentAuthStores(workspaceSlug);
      }
      return {
        agent: meResponse.agent ?? null,
        tokens: nextTokens,
      };
    })()
      .catch((error) => {
        clearAuthSession(storage, workspaceSlug);
        if (redirectOnFailure && browser) {
          window.location.assign(workspacePath(workspaceSlug, "/login"));
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
  workspaceSlug = getCurrentWorkspaceSlug(),
} = {}) {
  const state = ensureAuthState(workspaceSlug);
  if (!browser || !storage) {
    state.ready = true;
    syncCurrentAuthStores(workspaceSlug);
    return null;
  }

  state.ready = false;
  syncCurrentAuthStores(workspaceSlug);

  try {
    const result = await refreshAuthSession({
      fetchFn,
      storage,
      baseUrl,
      workspaceSlug,
      redirectOnFailure: false,
    });
    state.ready = true;
    syncCurrentAuthStores(workspaceSlug);
    return result;
  } catch {
    state.ready = true;
    syncCurrentAuthStores(workspaceSlug);
    return null;
  }
}

export function createAuthTokenProvider(fetchFn, options = {}) {
  const workspaceSlugProvider =
    options.workspaceSlugProvider ?? (() => getCurrentWorkspaceSlug());
  const baseUrl = options.baseUrl ?? "";

  return {
    getAccessToken() {
      return getAccessToken(workspaceSlugProvider());
    },
    hasRefreshToken(storage = sessionStorage) {
      return Boolean(loadStoredRefreshToken(storage, workspaceSlugProvider()));
    },
    async refreshAccessToken() {
      const result = await refreshAuthSession({
        fetchFn,
        baseUrl,
        workspaceSlug: workspaceSlugProvider(),
        redirectOnFailure: true,
      });
      return result?.tokens?.access_token ?? "";
    },
    async handleRefreshFailure() {
      const workspaceSlug = workspaceSlugProvider();
      clearAuthSession(undefined, workspaceSlug);
      if (browser) {
        window.location.assign(workspacePath(workspaceSlug, "/login"));
      }
    },
  };
}
