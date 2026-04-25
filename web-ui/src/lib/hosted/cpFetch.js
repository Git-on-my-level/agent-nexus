/**
 * Same-origin proxy to the control plane (see /hosted/api/*). Available when
 * web-ui is running in hosted mode (`ANX_CONTROL_BASE_URL` configured).
 * @param {string} path - e.g. `organizations` or `account/sessions/start` (no leading slash)
 * @param {RequestInit} [init]
 */
import { browser, dev } from "$app/environment";

import {
  CP_DEV_ACCESS_TOKEN_COOKIE,
  CP_TOKEN_MAX_AGE_SEC,
} from "$lib/hosted/cpSessionConstants.js";

export async function hostedCpFetch(path, init = {}) {
  const p = String(path ?? "").replace(/^\/+/, "");
  const headers = new Headers(init.headers ?? undefined);
  if (
    init.body &&
    !headers.has("content-type") &&
    typeof init.body === "string"
  ) {
    headers.set("content-type", "application/json");
  }
  return fetch(`/hosted/api/${p}`, {
    credentials: "include",
    ...init,
    headers,
  });
}

/**
 * Persist control-plane access token for SSR workspace resolution + API proxy.
 * Development: sets a JS-readable cookie. Production: POSTs to the shell so the
 * server can set an HttpOnly cookie.
 * @param {string} accessToken
 */
export async function persistHostedCpAccessToken(accessToken) {
  const token = String(accessToken ?? "").trim();
  if (!token || !browser) {
    return;
  }
  if (dev) {
    document.cookie = `${CP_DEV_ACCESS_TOKEN_COOKIE}=${encodeURIComponent(token)}; Path=/; Max-Age=${CP_TOKEN_MAX_AGE_SEC}; SameSite=Lax`;
    return;
  }
  const res = await fetch("/hosted/api/session", {
    method: "POST",
    credentials: "include",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ access_token: token }),
  });
  if (!res.ok) {
    const message = `Could not persist hosted session (${res.status}).`;
    throw new Error(message);
  }
}

export async function clearHostedCpAccessToken() {
  if (!browser) {
    return;
  }
  if (dev) {
    document.cookie = `${CP_DEV_ACCESS_TOKEN_COOKIE}=; Path=/; Max-Age=0; SameSite=Lax`;
  }
  try {
    await fetch("/hosted/api/session", {
      method: "DELETE",
      credentials: "include",
    });
  } catch {
    // best-effort — sign-out should not fail solely on cookie cleanup
  }
}
