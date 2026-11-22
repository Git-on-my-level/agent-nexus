import { env as privateEnv } from "$env/dynamic/private";

import { normalizeBaseUrl } from "$lib/config.js";
import { readHostedControlPlaneAccessToken } from "$lib/server/outOfWorkspace/cpSessionCookie.js";

const CONTROL_PLANE_WORKSPACE_AUTH_HEADER = "X-ANX-Control-Plane-Authorization";

export function hostedWorkspaceCoreBaseUrl({
  controlPlaneBaseUrl = privateEnv.ANX_CONTROL_BASE_URL,
  organizationSlug,
  workspaceSlug,
} = {}) {
  const base = normalizeBaseUrl(controlPlaneBaseUrl);
  const org = String(organizationSlug ?? "").trim();
  const ws = String(workspaceSlug ?? "").trim();
  if (!base || !org || !ws) {
    return "";
  }
  return new URL(
    `/ws/${encodeURIComponent(org)}/${encodeURIComponent(ws)}`,
    `${base}/`,
  ).toString();
}

export function hostedWorkspaceCoreProxyHeaders(
  event,
  env = privateEnv,
  headers = {},
) {
  const out = { ...headers };
  const token = readHostedControlPlaneAccessToken(event, env);
  if (token) {
    out[CONTROL_PLANE_WORKSPACE_AUTH_HEADER] = `Bearer ${token}`;
  }
  return out;
}
