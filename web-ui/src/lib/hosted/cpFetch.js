/**
 * Same-origin proxy to the control plane (see /hosted/api/*). Requires SaaS packed-host dev.
 * @param {string} path - e.g. `organizations` or `account/sessions/start` (no leading slash)
 * @param {RequestInit} [init]
 */
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

const CP_TOKEN_COOKIE = "oar_cp_dev_access_token";
const CP_TOKEN_MAX_AGE_SEC = 60 * 60 * 24 * 30;

/**
 * Persist control-plane access token for SSR workspace resolution + API proxy.
 * @param {string} accessToken
 */
export function persistHostedCpAccessToken(accessToken) {
  const token = String(accessToken ?? "").trim();
  if (!token || typeof document === "undefined") {
    return;
  }
  document.cookie = `${CP_TOKEN_COOKIE}=${encodeURIComponent(token)}; Path=/; Max-Age=${CP_TOKEN_MAX_AGE_SEC}; SameSite=Lax`;
}

export function clearHostedCpAccessToken() {
  if (typeof document === "undefined") {
    return;
  }
  document.cookie = `${CP_TOKEN_COOKIE}=; Path=/; Max-Age=0; SameSite=Lax`;
}
