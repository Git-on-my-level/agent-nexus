import { getWorkspaceHeader } from "../compat/workspaceCompat.js";
import { normalizeBaseUrl } from "../config.js";
import {
  DEFAULT_WORKSPACE_SLUG,
  WORKSPACE_HEADER,
  normalizeWorkspaceSlug,
} from "../workspacePaths.js";
import { createControlClient } from "./controlClient.js";
import {
  clearControlAccessToken,
  clearControlAccount,
  readControlAccessToken,
} from "./controlSession.js";
import {
  createWorkspaceCatalog,
  loadWorkspaceCatalog,
} from "./workspaceCatalog.js";

const CONTROL_WORKSPACE_CACHE_TTL_MS = 5000;
const controlWorkspaceCache = new Map();

function pruneExpiredControlWorkspaceCache(now = Date.now()) {
  for (const [accessToken, cacheEntry] of controlWorkspaceCache.entries()) {
    if (!cacheEntry || cacheEntry.expiresAt <= now) {
      controlWorkspaceCache.delete(accessToken);
    }
  }
}

function createWorkspaceNotConfiguredError(rawSlug) {
  const requestedSlug = String(rawSlug ?? "").trim();
  return {
    status: 404,
    payload: {
      error: {
        code: "workspace_not_configured",
        message: `Workspace '${requestedSlug}' is not configured in OAR_WORKSPACES.`,
      },
    },
  };
}

function createControlWorkspaceUnavailableError(rawSlug) {
  const requestedSlug = String(rawSlug ?? "").trim();
  return {
    status: 404,
    payload: {
      error: {
        code: "workspace_unavailable",
        message: `Workspace '${requestedSlug}' is not available in your control-plane workspace catalog. It may have been deleted, access may have been revoked, or provisioning may not be finished yet.`,
      },
    },
  };
}

function createControlResolutionError(error) {
  if (error?.status === 401) {
    return {
      status: 401,
      payload: {
        error: {
          code: "control_session_required",
          message:
            "Control-plane session expired while resolving workspace routing. Sign in again and retry.",
        },
      },
    };
  }

  const reason = error instanceof Error ? error.message : String(error);
  return {
    status: 503,
    payload: {
      error: {
        code: "workspace_resolution_unavailable",
        message:
          "Unable to resolve SaaS workspace routing from the control plane right now. Retry in a moment.",
        reason,
      },
    },
  };
}

function normalizeControlWorkspace(entry, index) {
  if (!entry || typeof entry !== "object") {
    throw new Error(
      `Control-plane workspace entry ${index + 1} must be an object.`,
    );
  }

  const slug = normalizeWorkspaceSlug(entry.slug);
  if (!slug) {
    throw new Error(
      `Control-plane workspace entry ${index + 1} is missing a valid slug.`,
    );
  }

  return {
    slug,
    label: String(entry.display_name ?? entry.label ?? slug).trim() || slug,
    description: String(entry.description ?? "").trim(),
    coreBaseUrl: normalizeBaseUrl(entry.core_origin ?? entry.coreBaseUrl),
    workspaceId: String(entry.workspace_id ?? entry.id ?? "").trim(),
    organizationId: String(entry.organization_id ?? "").trim(),
    workspacePath:
      String(entry.workspace_path ?? `/${slug}`).trim() || `/${slug}`,
    publicOrigin: String(entry.public_origin ?? "").trim(),
    source: "control-plane",
  };
}

function mergeWorkspaceCatalog(staticCatalog, controlWorkspaces) {
  const staticWorkspaces =
    staticCatalog.usesSyntheticDefaultWorkspace && controlWorkspaces.length > 0
      ? []
      : staticCatalog.workspaces;
  const mergedBySlug = new Map(
    staticWorkspaces.map((workspace) => [workspace.slug, workspace]),
  );

  for (const workspace of controlWorkspaces) {
    mergedBySlug.set(workspace.slug, workspace);
  }

  const mergedWorkspaces = Array.from(mergedBySlug.values());
  const defaultWorkspaceSlug =
    staticCatalog.usesSyntheticDefaultWorkspace && controlWorkspaces.length > 0
      ? (controlWorkspaces[0]?.slug ?? staticCatalog.defaultWorkspace.slug)
      : staticCatalog.defaultWorkspace.slug;

  return createWorkspaceCatalog({
    workspaces: mergedWorkspaces,
    defaultWorkspaceSlug,
    devActorMode: staticCatalog.devActorMode,
    usesSyntheticDefaultWorkspace:
      staticCatalog.usesSyntheticDefaultWorkspace &&
      controlWorkspaces.length === 0,
  });
}

async function loadControlWorkspaces(event, accessToken) {
  const now = Date.now();
  pruneExpiredControlWorkspaceCache(now);

  const cacheEntry = controlWorkspaceCache.get(accessToken);
  if (cacheEntry && cacheEntry.expiresAt > now) {
    return cacheEntry.workspaces;
  }

  try {
    const client = createControlClient(accessToken);
    const response = await client.listWorkspaces();
    const workspaces = (response.workspaces ?? []).map(
      normalizeControlWorkspace,
    );

    controlWorkspaceCache.set(accessToken, {
      expiresAt: now + CONTROL_WORKSPACE_CACHE_TTL_MS,
      workspaces,
    });
    return workspaces;
  } catch (error) {
    controlWorkspaceCache.delete(accessToken);
    if (error?.status === 401) {
      clearControlAccessToken(event);
      clearControlAccount(event);
    }
    throw error;
  }
}

async function loadMergedWorkspaceCatalog(event, staticCatalog) {
  const accessToken = readControlAccessToken(event);
  if (!accessToken) {
    return staticCatalog;
  }

  const controlWorkspaces = await loadControlWorkspaces(event, accessToken);
  return mergeWorkspaceCatalog(staticCatalog, controlWorkspaces);
}

export async function resolveWorkspaceCatalog(event) {
  const staticCatalog = loadWorkspaceCatalog();
  if (!event || !readControlAccessToken(event)) {
    return staticCatalog;
  }

  try {
    return await loadMergedWorkspaceCatalog(event, staticCatalog);
  } catch {
    return staticCatalog;
  }
}

export async function resolveWorkspaceBySlug({ event, workspaceSlug }) {
  const staticCatalog = loadWorkspaceCatalog();
  const rawSlug = String(workspaceSlug ?? "").trim();
  const hasControlSession = Boolean(readControlAccessToken(event));
  const normalizedSlug =
    rawSlug === "" ? DEFAULT_WORKSPACE_SLUG : normalizeWorkspaceSlug(rawSlug);

  if (!normalizedSlug) {
    return {
      catalog: staticCatalog,
      workspaceSlug: rawSlug,
      workspace: null,
      coreBaseUrl: "",
      error: hasControlSession
        ? createControlWorkspaceUnavailableError(rawSlug)
        : createWorkspaceNotConfiguredError(rawSlug),
    };
  }

  const staticWorkspace = staticCatalog.workspaceBySlug.get(normalizedSlug);

  if (staticWorkspace) {
    if (!hasControlSession) {
      return {
        catalog: staticCatalog,
        workspaceSlug: normalizedSlug,
        workspace: staticWorkspace,
        coreBaseUrl: staticWorkspace.coreBaseUrl,
        error: null,
      };
    }

    try {
      const mergedCatalog = await loadMergedWorkspaceCatalog(
        event,
        staticCatalog,
      );
      const workspace =
        mergedCatalog.workspaceBySlug.get(normalizedSlug) ?? staticWorkspace;
      return {
        catalog: mergedCatalog,
        workspaceSlug: normalizedSlug,
        workspace,
        coreBaseUrl: workspace.coreBaseUrl,
        error: null,
      };
    } catch {
      return {
        catalog: staticCatalog,
        workspaceSlug: normalizedSlug,
        workspace: staticWorkspace,
        coreBaseUrl: staticWorkspace.coreBaseUrl,
        error: null,
      };
    }
  }

  if (!hasControlSession) {
    return {
      catalog: staticCatalog,
      workspaceSlug: normalizedSlug,
      workspace: null,
      coreBaseUrl: "",
      error: createWorkspaceNotConfiguredError(rawSlug),
    };
  }

  try {
    const mergedCatalog = await loadMergedWorkspaceCatalog(
      event,
      staticCatalog,
    );
    const workspace = mergedCatalog.workspaceBySlug.get(normalizedSlug);
    if (!workspace) {
      return {
        catalog: mergedCatalog,
        workspaceSlug: normalizedSlug,
        workspace: null,
        coreBaseUrl: "",
        error: createControlWorkspaceUnavailableError(rawSlug),
      };
    }

    return {
      catalog: mergedCatalog,
      workspaceSlug: normalizedSlug,
      workspace,
      coreBaseUrl: workspace.coreBaseUrl,
      error: null,
    };
  } catch (error) {
    return {
      catalog: staticCatalog,
      workspaceSlug: normalizedSlug,
      workspace: null,
      coreBaseUrl: "",
      error: createControlResolutionError(error),
    };
  }
}

export async function resolveWorkspaceFromEvent(event) {
  const rawSlug =
    getWorkspaceHeader(event.request.headers) || event.params?.workspace || "";
  return resolveWorkspaceBySlug({
    event,
    workspaceSlug: rawSlug,
  });
}

export async function resolveProxyWorkspaceTarget({ event, workspaceSlug }) {
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
  controlWorkspaceCache.clear();
}

export function getWorkspaceResolutionCacheSize() {
  return controlWorkspaceCache.size;
}
