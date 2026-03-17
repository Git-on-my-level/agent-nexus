import { normalizeBaseUrl } from "../config.js";
import { WORKSPACE_HEADER } from "../workspacePaths.js";

export function resolveProxyWorkspaceTarget({ catalog, workspaceSlug }) {
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

  const workspace = catalog.workspaceBySlug.get(slug);
  if (!workspace) {
    return {
      status: 404,
      payload: {
        error: {
          code: "workspace_not_configured",
          message: `Workspace '${slug}' is not configured in OAR_WORKSPACES.`,
        },
      },
    };
  }

  return {
    workspace,
    coreBaseUrl: normalizeBaseUrl(workspace.coreBaseUrl),
  };
}

export const resolveProxyProjectTarget = resolveProxyWorkspaceTarget;

export function resolveProxyTarget({ catalog, workspaceSlug, projectSlug }) {
  const slug = workspaceSlug || projectSlug;
  return resolveProxyWorkspaceTarget({ catalog, workspaceSlug: slug });
}
