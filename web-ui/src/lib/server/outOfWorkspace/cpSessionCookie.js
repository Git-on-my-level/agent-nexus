import { dev } from "$app/environment";

import {
  CP_ACCESS_TOKEN_COOKIE,
  CP_DEV_ACCESS_TOKEN_COOKIE,
  CP_TOKEN_MAX_AGE_SEC,
} from "$lib/hosted/cpSessionConstants.js";

export {
  CP_ACCESS_TOKEN_COOKIE,
  CP_DEV_ACCESS_TOKEN_COOKIE,
  CP_TOKEN_MAX_AGE_SEC,
} from "$lib/hosted/cpSessionConstants.js";

/**
 * Bearer token from cookies only (HttpOnly session cookie, then dev cookie).
 * Used for `/hosted/api/*` proxy — never uses `ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN`.
 *
 * @param {import('@sveltejs/kit').RequestEvent | { cookies?: { get?: (name: string) => string | undefined } }} event
 */
export function readHostedControlPlaneProxyBearer(event) {
  const get = event?.cookies?.get;
  if (typeof get !== "function") {
    return "";
  }
  const fromSession = String(
    get.call(event.cookies, CP_ACCESS_TOKEN_COOKIE) ?? "",
  ).trim();
  if (fromSession) {
    return fromSession;
  }
  if (dev) {
    return String(
      get.call(event.cookies, CP_DEV_ACCESS_TOKEN_COOKIE) ?? "",
    ).trim();
  }
  return "";
}

/**
 * Resolve the control-plane bearer token for server-side CP calls: env bootstrap,
 * then cookies (same order as {@link readHostedControlPlaneProxyBearer}).
 *
 * @param {import('@sveltejs/kit').RequestEvent | { cookies?: { get?: (name: string) => string | undefined } }} event
 * @param {Record<string, string | undefined>} [env]
 */
export function readHostedControlPlaneAccessToken(event, env = {}) {
  const fromEnv = dev
    ? String(env.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN ?? "").trim()
    : "";
  if (fromEnv) {
    return fromEnv;
  }
  return readHostedControlPlaneProxyBearer(event);
}

/**
 * Refresh the browser cookie's idle lifetime after a successful hosted
 * control-plane request. The database session remains authoritative; this only
 * prevents the shell cookie from expiring earlier than the renewed server row.
 *
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {string} token
 */
export function renewHostedControlPlaneSessionCookie(event, token) {
  const normalized = String(token ?? "").trim();
  if (!normalized || dev || typeof event?.cookies?.set !== "function") {
    return;
  }
  event.cookies.set(CP_ACCESS_TOKEN_COOKIE, normalized, {
    path: "/",
    maxAge: CP_TOKEN_MAX_AGE_SEC,
    httpOnly: true,
    sameSite: "lax",
    secure: true,
  });
}
