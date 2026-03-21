import { get, writable } from "svelte/store";

import { normalizeBaseUrl } from "./config.js";

export const controlSessionReady = writable(false);
export const controlAccount = writable(null);
export const controlAuthenticated = writable(false);

const browser = typeof window !== "undefined";

const controlState = {
  ready: false,
  accessToken: "",
  account: null,
};

function syncControlStores() {
  controlSessionReady.set(controlState.ready);
  controlAccount.set(controlState.account);
  controlAuthenticated.set(
    Boolean(controlState.accessToken && controlState.account),
  );
}

function resolveFetch(fetchFn) {
  if (typeof fetchFn === "function") {
    return fetchFn;
  }
  return globalThis.fetch.bind(globalThis);
}

function buildUrl(pathname, baseUrl = "") {
  const resolvedBaseUrl = normalizeBaseUrl(baseUrl);
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

export function getControlAccessToken() {
  return controlState.accessToken;
}

export function getControlAccount() {
  return get(controlAccount);
}

export function isControlAuthenticated() {
  return get(controlAuthenticated);
}

export function completeControlSession(account, accessToken) {
  controlState.accessToken = accessToken || "";
  controlState.account = account ?? null;
  controlState.ready = true;
  syncControlStores();
  return { account: account ?? null };
}

export function clearControlSession() {
  controlState.accessToken = "";
  controlState.account = null;
  controlState.ready = true;
  syncControlStores();
}

export async function initializeControlSession({ fetchFn, baseUrl = "" } = {}) {
  controlState.ready = false;
  syncControlStores();

  try {
    const result = await requestJSON("/control/session", {
      fetchFn,
      baseUrl,
    });
    controlState.account = result.account ?? null;
    controlState.accessToken = result.access_token || "";
    controlState.ready = true;
    syncControlStores();
    return result.account ?? null;
  } catch {
    controlState.ready = true;
    syncControlStores();
    return null;
  }
}

export async function logoutControlSession({ fetchFn, baseUrl = "" } = {}) {
  if (browser || typeof fetchFn === "function") {
    try {
      await requestJSON("/control/session", {
        fetchFn,
        baseUrl,
        method: "DELETE",
      });
    } catch {
      // Fall through to local cleanup.
    }
  }

  clearControlSession();
}

export function createControlTokenProvider() {
  return {
    getAccessToken() {
      return controlState.accessToken;
    },
  };
}
