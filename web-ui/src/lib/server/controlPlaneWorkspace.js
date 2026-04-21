import { normalizeBaseUrl } from "$lib/config.js";
import {
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths.js";

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

/**
 * Map a workspace row from the control-plane API into the shape consumed by
 * the web-ui resolver. Returns null only when the row is missing essential
 * identifiers (slug). When `core_origin` is not yet populated (e.g. the
 * workspace is still provisioning, suspended, or its core process has not yet
 * registered an origin), we still return the entry with an empty
 * `coreBaseUrl` and a `status` hint so callers can distinguish
 * "workspace doesn't exist" from "workspace exists but isn't ready" and show
 * an accurate error to the user.
 */
function mapWorkspaceRowFromControlPlane(match) {
  if (!match || typeof match !== "object") {
    return null;
  }
  const normalized = normalizeWorkspaceSlug(match.slug);
  if (!normalized) {
    return null;
  }
  const coreBaseUrl = normalizeBaseUrl(match.core_origin ?? "");
  const organizationId = String(match.organization_id ?? "").trim();
  const organizationSlug = normalizeOrganizationSlug(
    match.organization_slug ?? match.organizationSlug ?? "",
  );
  const status = String(match.status ?? "").trim();
  const desiredState = String(match.desired_state ?? "").trim();
  return {
    organizationSlug,
    slug: normalized,
    label: String(match.display_name ?? match.slug ?? normalized).trim(),
    description: String(match.description ?? "").trim(),
    coreBaseUrl,
    publicOrigin: normalizeBaseUrl(match.public_origin ?? ""),
    id: String(match.id ?? "").trim(),
    workspaceId: String(match.id ?? "").trim(),
    organizationId,
    status,
    desiredState,
  };
}

function controlPlaneDevAccessToken(env, getCookie) {
  let token = String(env?.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN ?? "").trim();
  if (token || typeof getCookie !== "function") {
    return token;
  }
  return String(getCookie("oar_cp_dev_access_token") ?? "").trim();
}

export async function resolveControlPlaneOrganizationIdBySlug({
  env,
  organizationSlug,
  fetchFn = fetch,
  getCookie,
}) {
  if (!isSaasPackedHostDev(env)) {
    return "";
  }
  const base = normalizeBaseUrl(env.ANX_CONTROL_BASE_URL);
  const token = controlPlaneDevAccessToken(env, getCookie);
  const normalizedOrg = normalizeOrganizationSlug(organizationSlug);
  if (!base || !token || !normalizedOrg) {
    return "";
  }

  let cursor = "";
  try {
    for (let page = 0; page < 20; page++) {
      const url = new URL(`${base}/organizations`);
      url.searchParams.set("limit", "200");
      if (cursor) {
        url.searchParams.set("cursor", cursor);
      }
      const res = await fetchFn(url.toString(), {
        headers: { authorization: `Bearer ${token}` },
      });
      if (!res.ok) {
        return "";
      }
      const body = await res.json();
      const orgs = body.organizations ?? [];
      const match = orgs.find(
        (row) => normalizeOrganizationSlug(row.slug) === normalizedOrg,
      );
      if (match) {
        return String(match.id ?? "").trim();
      }
      cursor = String(body.next_cursor ?? "").trim();
      if (!cursor) {
        break;
      }
    }
  } catch {
    return "";
  }
  return "";
}

/**
 * Fetches workspace metadata from the hosted control plane (local or remote).
 * Used when ANX_SAAS_PACKED_HOST_DEV is set and the static catalog has no entry
 * for the slug — e.g. after creating a workspace via the control plane API.
 */
export async function fetchWorkspaceEntryFromControlPlane({
  env,
  organizationSlug,
  workspaceSlug,
  fetchFn = fetch,
  getCookie,
}) {
  if (!isSaasPackedHostDev(env)) {
    return null;
  }
  const base = normalizeBaseUrl(env.ANX_CONTROL_BASE_URL);
  const token = controlPlaneDevAccessToken(env, getCookie);
  const normalizedOrg = normalizeOrganizationSlug(organizationSlug);
  const normalized = normalizeWorkspaceSlug(workspaceSlug);
  if (!base || !token || !normalizedOrg || !normalized) {
    return null;
  }

  const organizationId = await resolveControlPlaneOrganizationIdBySlug({
    env,
    organizationSlug: normalizedOrg,
    fetchFn,
    getCookie,
  });
  if (!organizationId) {
    return null;
  }

  let cursor = "";
  try {
    for (let page = 0; page < 20; page++) {
      const url = new URL(`${base}/workspaces`);
      url.searchParams.set("organization_id", organizationId);
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
      const match = batch.find(
        (row) => normalizeWorkspaceSlug(row.slug) === normalized,
      );
      if (match) {
        return mapWorkspaceRowFromControlPlane(match);
      }
      cursor = String(body.next_cursor ?? "").trim();
      if (!cursor) {
        break;
      }
    }
  } catch {
    return null;
  }

  return null;
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
        if (mapped && mapped.coreBaseUrl) {
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
