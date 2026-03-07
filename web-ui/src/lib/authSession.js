import { get, writable } from "svelte/store";

import { clearSelectedActor } from "./actorSession.js";
import { normalizeBaseUrl, oarCoreBaseUrl } from "./config.js";

export const REFRESH_TOKEN_STORAGE_KEY = "oar_ui_refresh_token";

export const authSessionReady = writable(false);
export const authenticatedAgent = writable(null);

const browser = typeof window !== "undefined";

let accessToken = "";
let refreshPromise;

function resolveBaseUrl(baseUrl = oarCoreBaseUrl) {
  return normalizeBaseUrl(baseUrl);
}

function resolveFetch(fetchFn) {
  if (typeof fetchFn === "function") {
    return fetchFn;
  }

  return globalThis.fetch.bind(globalThis);
}

function buildUrl(pathname, baseUrl = oarCoreBaseUrl) {
  const resolvedBaseUrl = resolveBaseUrl(baseUrl);
  if (!resolvedBaseUrl) {
    return pathname;
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

function setAccessToken(nextToken) {
  accessToken = String(nextToken ?? "").trim();
}

export function getAccessToken() {
  return accessToken;
}

export function loadStoredRefreshToken(storage = sessionStorage) {
  return storage.getItem(REFRESH_TOKEN_STORAGE_KEY) ?? "";
}

export function saveRefreshToken(refreshToken, storage = sessionStorage) {
  const normalized = String(refreshToken ?? "").trim();
  if (!normalized) {
    storage.removeItem(REFRESH_TOKEN_STORAGE_KEY);
    return "";
  }

  storage.setItem(REFRESH_TOKEN_STORAGE_KEY, normalized);
  return normalized;
}

export function clearAuthSession(
  storage = browser ? sessionStorage : undefined,
) {
  setAccessToken("");
  if (storage) {
    storage.removeItem(REFRESH_TOKEN_STORAGE_KEY);
  }
  authenticatedAgent.set(null);
  if (browser) {
    clearSelectedActor();
  }
}

export function completeAuthSession(agent, tokens, storage = sessionStorage) {
  setAccessToken(tokens?.access_token);
  saveRefreshToken(tokens?.refresh_token, storage);
  authenticatedAgent.set(agent ?? null);
  authSessionReady.set(true);
  return {
    agent: agent ?? null,
    tokens,
  };
}

export function getAuthenticatedAgent() {
  return get(authenticatedAgent);
}

export function getAuthenticatedActorId() {
  return getAuthenticatedAgent()?.actor_id ?? "";
}

export function isAuthenticated() {
  return Boolean(getAuthenticatedAgent()?.agent_id);
}

export async function refreshAuthSession({
  fetchFn,
  storage = sessionStorage,
  baseUrl = oarCoreBaseUrl,
  redirectOnFailure = false,
} = {}) {
  const refreshToken = loadStoredRefreshToken(storage);
  if (!refreshToken) {
    clearAuthSession(storage);
    return null;
  }

  if (!refreshPromise) {
    refreshPromise = (async () => {
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
      setAccessToken(nextTokens.access_token);
      saveRefreshToken(nextTokens.refresh_token, storage);

      const meResponse = await requestJSON("/agents/me", {
        fetchFn,
        baseUrl,
        token: nextTokens.access_token,
      });

      authenticatedAgent.set(meResponse.agent ?? null);
      return {
        agent: meResponse.agent ?? null,
        tokens: nextTokens,
      };
    })()
      .catch((error) => {
        clearAuthSession(storage);
        if (redirectOnFailure && browser) {
          window.location.assign("/login");
        }
        throw error;
      })
      .finally(() => {
        refreshPromise = undefined;
      });
  }

  return refreshPromise;
}

export async function initializeAuthSession({
  fetchFn,
  storage = browser ? sessionStorage : undefined,
  baseUrl = oarCoreBaseUrl,
} = {}) {
  if (!browser || !storage) {
    authSessionReady.set(true);
    return null;
  }

  authSessionReady.set(false);

  try {
    const result = await refreshAuthSession({
      fetchFn,
      storage,
      baseUrl,
      redirectOnFailure: false,
    });
    authSessionReady.set(true);
    return result;
  } catch {
    authSessionReady.set(true);
    return null;
  }
}

export function createAuthTokenProvider(fetchFn, baseUrl = oarCoreBaseUrl) {
  return {
    getAccessToken() {
      return getAccessToken();
    },
    hasRefreshToken(storage = sessionStorage) {
      return Boolean(loadStoredRefreshToken(storage));
    },
    async refreshAccessToken() {
      const result = await refreshAuthSession({
        fetchFn,
        baseUrl,
        redirectOnFailure: true,
      });
      return result?.tokens?.access_token ?? "";
    },
    async handleRefreshFailure() {
      clearAuthSession();
      if (browser) {
        window.location.assign("/login");
      }
    },
  };
}
