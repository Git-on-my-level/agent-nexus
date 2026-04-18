import { normalizeBaseUrl } from "$lib/config.js";
import { normalizeWorkspaceSlug } from "$lib/workspacePaths.js";

export function isSaasPackedHostDev(env) {
  const v = String(env?.ANX_SAAS_PACKED_HOST_DEV ?? "")
    .trim()
    .toLowerCase();
  return v === "1" || v === "true";
}

/** True when the SvelteKit shell should show hosted-only UI (CP-backed SaaS). */
export function isHostedWebUiShell(env) {
  return (
    isSaasPackedHostDev(env) &&
    Boolean(normalizeBaseUrl(env?.ANX_CONTROL_BASE_URL ?? ""))
  );
}

function mapWorkspaceRowFromControlPlane(match) {
  if (!match || typeof match !== "object") {
    return null;
  }
  const normalized = normalizeWorkspaceSlug(match.slug);
  if (!normalized) {
    return null;
  }
  const coreBaseUrl = normalizeBaseUrl(match.core_origin ?? "");
  if (!coreBaseUrl) {
    return null;
  }
  const organizationId = String(match.organization_id ?? "").trim();
  return {
    slug: normalized,
    label: String(match.display_name ?? match.slug ?? normalized).trim(),
    description: String(match.description ?? "").trim(),
    coreBaseUrl,
    publicOrigin: normalizeBaseUrl(match.public_origin ?? ""),
    id: String(match.id ?? "").trim(),
    workspaceId: String(match.id ?? "").trim(),
    organizationId,
  };
}

function controlPlaneDevAccessToken(env, getCookie) {
  let token = String(env?.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN ?? "").trim();
  if (token || typeof getCookie !== "function") {
    return token;
  }
  return String(getCookie("oar_cp_dev_access_token") ?? "").trim();
}

/**
 * Fetches workspace metadata from the hosted control plane (local or remote).
 * Used when ANX_SAAS_PACKED_HOST_DEV is set and the static catalog has no entry
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
  const base = normalizeBaseUrl(env.ANX_CONTROL_BASE_URL);
  const token = controlPlaneDevAccessToken(env, getCookie);
  const normalized = normalizeWorkspaceSlug(workspaceSlug);
  if (!base || !token || !normalized) {
    return null;
  }

  const workspaces = [];
  let cursor = "";
  try {
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
  } catch {
    return null;
  }

  const match = workspaces.find(
    (row) => normalizeWorkspaceSlug(row.slug) === normalized,
  );
  if (!match) {
    return null;
  }

  return mapWorkspaceRowFromControlPlane(match);
}

/**
 * Lists workspaces for an organization from the hosted control plane.
 * Returns the same entry shape as fetchWorkspaceEntryFromControlPlane.
 */
export async function fetchWorkspaceListFromControlPlane({
  env,
  organizationID,
  fetchFn = fetch,
  getCookie,
}) {
  if (!isSaasPackedHostDev(env)) {
    return [];
  }
  const base = normalizeBaseUrl(env.ANX_CONTROL_BASE_URL);
  const token = controlPlaneDevAccessToken(env, getCookie);
  const org = String(organizationID ?? "").trim();
  if (!base || !token || !org) {
    return [];
  }

  const out = [];
  let cursor = "";
  try {
    for (let page = 0; page < 20; page++) {
      const url = new URL(`${base}/workspaces`);
      url.searchParams.set("organization_id", org);
      url.searchParams.set("limit", "200");
      if (cursor) {
        url.searchParams.set("cursor", cursor);
      }
      const res = await fetchFn(url.toString(), {
        headers: { authorization: `Bearer ${token}` },
      });
      if (!res.ok) {
        return [];
      }
      const body = await res.json();
      const batch = body.workspaces ?? [];
      for (const row of batch) {
        const mapped = mapWorkspaceRowFromControlPlane(row);
        if (mapped) {
          out.push(mapped);
        }
      }
      cursor = String(body.next_cursor ?? "").trim();
      if (!cursor) {
        break;
      }
    }
  } catch {
    return [];
  }
  return out;
}
