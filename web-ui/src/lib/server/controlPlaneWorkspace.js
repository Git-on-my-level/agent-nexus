import { normalizeBaseUrl } from "$lib/config.js";
import { normalizeWorkspaceSlug } from "$lib/workspacePaths.js";

export function isSaasPackedHostDev(env) {
  const v = String(env?.OAR_SAAS_PACKED_HOST_DEV ?? "")
    .trim()
    .toLowerCase();
  return v === "1" || v === "true";
}

function controlPlaneDevAccessToken(env, getCookie) {
  let token = String(env?.OAR_CONTROL_PLANE_DEV_ACCESS_TOKEN ?? "").trim();
  if (token || typeof getCookie !== "function") {
    return token;
  }
  return String(getCookie("oar_cp_dev_access_token") ?? "").trim();
}

/**
 * Fetches workspace metadata from the hosted control plane (local or remote).
 * Used when OAR_SAAS_PACKED_HOST_DEV is set and the static catalog has no entry
 * for the slug — e.g. after creating a workspace via the control plane API.
 */
export async function fetchWorkspaceEntryFromControlPlane({
  env,
  workspaceSlug,
  fetchFn = fetch,
  getCookie,
}) {
  if (!isSaasPackedHostDev(env)) {
    return null;
  }
  const base = normalizeBaseUrl(env.OAR_CONTROL_BASE_URL);
  const token = controlPlaneDevAccessToken(env, getCookie);
  const normalized = normalizeWorkspaceSlug(workspaceSlug);
  if (!base || !token || !normalized) {
    return null;
  }

  const workspaces = [];
  let cursor = "";
  for (let page = 0; page < 20; page++) {
    const url = new URL(`${base}/workspaces`);
    url.searchParams.set("limit", "200");
    if (cursor) {
      url.searchParams.set("cursor", cursor);
    }
    const res = await fetchFn(url.toString(), {
      headers: { authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      return null;
    }
    const body = await res.json();
    const batch = body.workspaces ?? [];
    workspaces.push(...batch);
    cursor = String(body.next_cursor ?? "").trim();
    if (!cursor) {
      break;
    }
  }

  const match = workspaces.find(
    (row) => normalizeWorkspaceSlug(row.slug) === normalized,
  );
  if (!match) {
    return null;
  }

  const coreBaseUrl = normalizeBaseUrl(match.core_origin ?? "");
  if (!coreBaseUrl) {
    return null;
  }

  return {
    slug: normalized,
    label: String(match.display_name ?? match.slug ?? normalized).trim(),
    description: "",
    coreBaseUrl,
    publicOrigin: normalizeBaseUrl(match.public_origin ?? ""),
    id: String(match.id ?? "").trim(),
    workspaceId: String(match.id ?? "").trim(),
  };
}
