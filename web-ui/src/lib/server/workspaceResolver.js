import { env as privateEnv } from "$env/dynamic/private";
import { getWorkspaceHeader } from "../compat/workspaceCompat.js";
import { normalizeBaseUrl } from "../config.js";
import {
  DEFAULT_WORKSPACE_SLUG,
  WORKSPACE_HEADER,
  normalizeWorkspaceSlug,
} from "../workspacePaths.js";
import { fetchWorkspaceEntryFromControlPlane } from "./controlPlaneWorkspace.js";
import { loadWorkspaceCatalog } from "./workspaceCatalog.js";

function createWorkspaceNotConfiguredError(rawSlug) {
  const requestedSlug = String(rawSlug ?? "").trim();
  return {
    status: 404,
    payload: {
      error: {
        code: "workspace_not_configured",
        message: `Workspace '${requestedSlug}' is not configured in OAR_WORKSPACES and could not be resolved from the control plane.`,
      },
    },
  };
}

export async function resolveWorkspaceCatalog() {
  return loadWorkspaceCatalog();
}

export async function resolveWorkspaceBySlug({ workspaceSlug, event }) {
  const catalog = loadWorkspaceCatalog();
  const rawSlug = String(workspaceSlug ?? "").trim();
  const normalizedSlug =
    rawSlug === "" ? DEFAULT_WORKSPACE_SLUG : normalizeWorkspaceSlug(rawSlug);

  if (!normalizedSlug) {
    return {
      catalog,
      workspaceSlug: rawSlug,
      workspace: null,
      coreBaseUrl: "",
      error: createWorkspaceNotConfiguredError(rawSlug),
    };
  }

  let workspace = catalog.workspaceBySlug.get(normalizedSlug);
  if (!workspace) {
    const fetchFn = event?.fetch ?? fetch;
    const cpWorkspace = await fetchWorkspaceEntryFromControlPlane({
      env: privateEnv,
      workspaceSlug: normalizedSlug,
      fetchFn,
      getCookie:
        event && typeof event.cookies?.get === "function"
          ? (name) => event.cookies.get(name)
          : undefined,
    });
    if (cpWorkspace) {
      workspace = cpWorkspace;
    }
  }

  if (!workspace) {
    return {
      catalog,
      workspaceSlug: normalizedSlug,
      workspace: null,
      coreBaseUrl: "",
      error: createWorkspaceNotConfiguredError(rawSlug),
    };
  }

  return {
    catalog,
    workspaceSlug: normalizedSlug,
    workspace,
    coreBaseUrl: workspace.coreBaseUrl,
    error: null,
  };
}

export async function resolveWorkspaceFromEvent(event) {
  const rawSlug =
    getWorkspaceHeader(event.request.headers) || event.params?.workspace || "";
  return resolveWorkspaceBySlug({
    event,
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

  const resolved = await resolveWorkspaceBySlug({
    event,
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
