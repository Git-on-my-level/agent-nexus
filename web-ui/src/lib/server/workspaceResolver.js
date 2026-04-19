import { env as privateEnv } from "$env/dynamic/private";
import {
  getOrganizationHeader,
  getWorkspaceHeader,
  readOrganizationSlugHeader,
} from "../compat/workspaceCompat.js";
import { normalizeBaseUrl } from "../config.js";
import {
  WORKSPACE_HEADER,
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
  workspaceCompositeKey,
} from "../workspacePaths.js";
import {
  fetchWorkspaceEntryFromControlPlane,
  fetchWorkspaceListFromControlPlane,
  isHostedWebUiShell,
} from "./controlPlaneWorkspace.js";
import {
  createWorkspaceCatalog,
  loadWorkspaceCatalog,
} from "./workspaceCatalog.js";

function createWorkspaceNotConfiguredError(rawOrg, rawWs) {
  const org = String(rawOrg ?? "").trim();
  const ws = String(rawWs ?? "").trim();
  const label = org && ws ? `'${org}/${ws}'` : ws ? `'${ws}'` : `'${org}'`;
  return {
    status: 404,
    payload: {
      error: {
        code: "workspace_not_configured",
        message: `Workspace ${label} is not configured in ANX_WORKSPACES and could not be resolved from the control plane.`,
      },
    },
  };
}

function createWorkspaceRouteIncompleteError() {
  return {
    status: 404,
    payload: {
      error: {
        code: "workspace_route_incomplete",
        message:
          "Organization and workspace segments are required in the URL path.",
      },
    },
  };
}

function mergeWorkspaceEntriesByComposite(primary, secondary) {
  const map = new Map();
  for (const w of primary) {
    const k = workspaceCompositeKey(w.organizationSlug, w.slug);
    if (k) {
      map.set(k, { ...w, organizationSlug: w.organizationSlug, slug: w.slug });
    }
  }
  for (const w of secondary) {
    const k = workspaceCompositeKey(w.organizationSlug, w.slug);
    if (!k) {
      continue;
    }
    const existing = map.get(k);
    if (existing) {
      map.set(k, {
        ...existing,
        ...w,
        organizationSlug: w.organizationSlug ?? existing.organizationSlug,
        slug: w.slug,
      });
    } else {
      map.set(k, w);
    }
  }
  return Array.from(map.values());
}

export async function resolveWorkspaceCatalog(event) {
  const base = loadWorkspaceCatalog();
  if (!isHostedWebUiShell(privateEnv)) {
    return base;
  }

  const orgRaw = String(event?.params?.organization ?? "").trim();
  const slug = String(event?.params?.workspace ?? "").trim();
  if (!orgRaw || !slug) {
    return base;
  }

  const fetchFn = event?.fetch ?? fetch;
  const getCookie =
    event && typeof event.cookies?.get === "function"
      ? (name) => event.cookies.get(name)
      : undefined;

  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: orgRaw,
    workspaceSlug: slug,
  });
  if (resolved.error || !resolved.workspace) {
    return base;
  }

  let organizationId = String(resolved.workspace.organizationId ?? "").trim();
  if (!organizationId) {
    const cp = await fetchWorkspaceEntryFromControlPlane({
      env: privateEnv,
      organizationSlug: resolved.workspace.organizationSlug,
      workspaceSlug: resolved.workspace.slug,
      fetchFn,
      getCookie,
    });
    organizationId = String(cp?.organizationId ?? "").trim();
  }
  if (!organizationId) {
    return base;
  }

  const fromOrg = await fetchWorkspaceListFromControlPlane({
    env: privateEnv,
    organizationID: organizationId,
    fetchFn,
    getCookie,
  });
  if (fromOrg.length === 0) {
    return base;
  }

  const merged = mergeWorkspaceEntriesByComposite(base.workspaces, fromOrg);
  return createWorkspaceCatalog({
    workspaces: merged,
    defaultWorkspaceSlug: resolved.workspace.slug,
    defaultOrganizationSlug: resolved.workspace.organizationSlug,
    devActorMode: base.devActorMode,
    usesSyntheticDefaultWorkspace: base.usesSyntheticDefaultWorkspace,
    hostedDevAllowEmpty: false,
  });
}

export async function resolveWorkspaceInRoute({
  organizationSlug,
  workspaceSlug,
  event,
}) {
  const catalog = loadWorkspaceCatalog();
  const rawOrg = String(organizationSlug ?? "").trim();
  const rawSlug = String(workspaceSlug ?? "").trim();

  if (rawOrg === "" || rawSlug === "") {
    return {
      catalog,
      organizationSlug: rawOrg,
      workspaceSlug: rawSlug,
      workspace: null,
      coreBaseUrl: "",
      error: createWorkspaceRouteIncompleteError(),
    };
  }

  const normalizedOrg = normalizeOrganizationSlug(rawOrg);
  const normalizedSlug = normalizeWorkspaceSlug(rawSlug);

  if (!normalizedOrg || !normalizedSlug) {
    return {
      catalog,
      organizationSlug: rawOrg,
      workspaceSlug: rawSlug,
      workspace: null,
      coreBaseUrl: "",
      error: createWorkspaceNotConfiguredError(rawOrg, rawSlug),
    };
  }

  const compositeKey = workspaceCompositeKey(normalizedOrg, normalizedSlug);
  let workspace = catalog.workspaceByComposite.get(compositeKey);
  if (!workspace) {
    const fetchFn = event?.fetch ?? fetch;
    const cpWorkspace = await fetchWorkspaceEntryFromControlPlane({
      env: privateEnv,
      organizationSlug: normalizedOrg,
      workspaceSlug: normalizedSlug,
      fetchFn,
      getCookie:
        event && typeof event.cookies?.get === "function"
          ? (name) => event.cookies.get(name)
          : undefined,
    });
    if (cpWorkspace) {
      workspace = {
        organizationSlug: cpWorkspace.organizationSlug || normalizedOrg,
        slug: cpWorkspace.slug,
        label: cpWorkspace.label,
        description: cpWorkspace.description,
        coreBaseUrl: cpWorkspace.coreBaseUrl,
        publicOrigin: cpWorkspace.publicOrigin,
        id: cpWorkspace.id,
        workspaceId: cpWorkspace.workspaceId,
        organizationId: cpWorkspace.organizationId,
      };
    }
  }

  if (!workspace) {
    return {
      catalog,
      organizationSlug: normalizedOrg,
      workspaceSlug: normalizedSlug,
      workspace: null,
      coreBaseUrl: "",
      error: createWorkspaceNotConfiguredError(rawOrg, rawSlug),
    };
  }

  return {
    catalog,
    organizationSlug: workspace.organizationSlug,
    workspaceSlug: workspace.slug,
    workspace,
    coreBaseUrl: workspace.coreBaseUrl,
    error: null,
  };
}

export async function resolveWorkspaceFromEvent(event) {
  const paramOrg = String(event.params?.organization ?? "").trim();
  const paramWs = String(event.params?.workspace ?? "").trim();
  const headerOrg = readOrganizationSlugHeader(event.request.headers);
  const headerWs = getWorkspaceHeader(event.request.headers);
  const rawOrg = paramOrg || headerOrg;
  const rawSlug = paramWs || String(headerWs ?? "").trim();
  return resolveWorkspaceInRoute({
    event,
    organizationSlug: rawOrg,
    workspaceSlug: rawSlug,
  });
}

export async function resolveProxyWorkspaceTarget({ workspaceSlug, event }) {
  const slug = String(workspaceSlug ?? "").trim();
  if (!slug) {
    return {
      status: 400,
      payload: {
        error: {
          code: "workspace_header_required",
          message: `Missing ${WORKSPACE_HEADER} header on proxied request.`,
        },
      },
    };
  }

  const orgSlug = event?.request
    ? getOrganizationHeader(event.request.headers)
    : "";
  const trimmedOrg = String(orgSlug ?? "").trim();
  if (!trimmedOrg) {
    return {
      status: 400,
      payload: {
        error: {
          code: "organization_header_required",
          message: "Missing x-anx-organization-slug header on proxied request.",
        },
      },
    };
  }

  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: trimmedOrg,
    workspaceSlug: slug,
  });
  if (resolved.error) {
    return {
      status: resolved.error.status,
      payload: resolved.error.payload,
    };
  }

  return {
    workspace: resolved.workspace,
    coreBaseUrl: normalizeBaseUrl(resolved.workspace.coreBaseUrl),
  };
}

export function clearWorkspaceResolutionCache() {
  // No-op: OSS resolver is static and does not keep session-bound caches.
}

export function getWorkspaceResolutionCacheSize() {
  return 0;
}
