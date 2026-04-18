import { error } from "@sveltejs/kit";
import { env as privateEnv } from "$env/dynamic/private";

import { normalizeBaseUrl } from "$lib/config.js";
import { allowHostedControlPlanePath } from "$lib/server/hostedControlPlaneAllowlist.js";
import { isSaasPackedHostDev } from "$lib/server/controlPlaneWorkspace.js";

export { allowHostedControlPlanePath } from "$lib/server/hostedControlPlaneAllowlist.js";

/**
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {string} method
 */
export async function proxyHostedControlPlaneRequest(event, method) {
  if (!isSaasPackedHostDev(privateEnv)) {
    throw error(404, "Not found");
  }
  const base = normalizeBaseUrl(privateEnv.ANX_CONTROL_BASE_URL);
  if (!base) {
    throw error(503, "Control plane URL is not configured");
  }

  const segments = /** @type {string} */ (event.params.segments ?? "");
  const subpath = String(segments).replace(/\/+$/, "");
  if (!allowHostedControlPlanePath(subpath)) {
    throw error(403, "Forbidden");
  }

  const target = `${base}/${subpath}${event.url.search}`;
  const headers = new Headers();
  const origin = event.request.headers.get("origin");
  if (origin) {
    headers.set("origin", origin);
  }
  const fwdAuth = event.request.headers.get("authorization");
  const cookieToken = event.cookies.get("oar_cp_dev_access_token");
  if (fwdAuth) {
    headers.set("authorization", fwdAuth);
  } else if (cookieToken) {
    headers.set("authorization", `Bearer ${cookieToken}`);
  }
  const contentType = event.request.headers.get("content-type");
  if (contentType) {
    headers.set("content-type", contentType);
  }

  /** @type {RequestInit} */
  const init = { method, headers };
  if (method !== "GET" && method !== "HEAD") {
    const buf = await event.request.arrayBuffer();
    if (buf.byteLength > 0) {
      init.body = buf;
    }
  }

  const res = await fetch(target, init);
  const outHeaders = new Headers(res.headers);
  outHeaders.delete("content-encoding");
  outHeaders.delete("transfer-encoding");
  return new Response(res.body, {
    status: res.status,
    statusText: res.statusText,
    headers: outHeaders,
  });
}
