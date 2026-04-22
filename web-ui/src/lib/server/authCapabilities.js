import { normalizeBaseUrl } from "$lib/config.js";

function isSaasPackedHostDevFlag(env) {
  const v = String(env?.ANX_SAAS_PACKED_HOST_DEV ?? "")
    .trim()
    .toLowerCase();
  return v === "1" || v === "true";
}

/**
 * @typedef {'local' | 'packed-host-dev' | 'hosted'} AuthCapabilityMode
 */

/**
 * Single probe for how the web UI can talk to the control plane and related
 * services. Replaces ad-hoc `process.env` checks across hooks, callbacks, and
 * resolvers.
 *
 * - **local**: No `ANX_CONTROL_BASE_URL` — no CP-backed catalog or workspace-id lookup.
 * - **packed-host-dev**: `ANX_SAAS_PACKED_HOST_DEV` + control plane URL — same as
 *   {@link isHostedWebUiShell} ingredients; supports `fetchWorkspaceByIdFromControlPlane`.
 * - **hosted**: Control plane URL without the packed-host flag — production-like CP
 *   URL; workspace-id lookup from the BFF may be disabled unless extended later.
 *
 * @param {Record<string, unknown>} env
 * @returns {{
 *   mode: AuthCapabilityMode,
 *   controlPlaneUrl: string,
 *   reason: string,
 *   supportsCpWorkspaceIdLookup: boolean,
 * }}
 */
export function resolveAuthCapabilities(env) {
  const controlPlaneUrl = normalizeBaseUrl(env?.ANX_CONTROL_BASE_URL ?? "");
  if (!controlPlaneUrl) {
    return {
      mode: "local",
      controlPlaneUrl: "",
      reason: "ANX_CONTROL_BASE_URL is unset",
      supportsCpWorkspaceIdLookup: false,
    };
  }

  const packed = isSaasPackedHostDevFlag(env);
  if (packed) {
    return {
      mode: "packed-host-dev",
      controlPlaneUrl,
      reason: "ANX_SAAS_PACKED_HOST_DEV with ANX_CONTROL_BASE_URL",
      supportsCpWorkspaceIdLookup: true,
    };
  }

  return {
    mode: "hosted",
    controlPlaneUrl,
    reason: "ANX_CONTROL_BASE_URL without packed-host dev flag",
    supportsCpWorkspaceIdLookup: false,
  };
}

/** True when the SvelteKit shell should show hosted-only UI (CP-backed SaaS). */
export function isHostedWebUiShell(env) {
  return resolveAuthCapabilities(env).mode === "packed-host-dev";
}
