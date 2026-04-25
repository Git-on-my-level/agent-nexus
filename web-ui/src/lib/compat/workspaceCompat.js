import { normalizeWorkspaceSlug } from "$lib/workspacePaths";

const ORGANIZATION_HEADER = "x-anx-organization-slug";

/**
 * Resolves workspace catalog from env. Only `ANX_WORKSPACES` and
 * `ANX_DEFAULT_WORKSPACE` are read; legacy `ANX_PROJECTS` / `ANX_DEFAULT_PROJECT`
 * are not supported.
 */
export function resolveWorkspaceEnv(env) {
  return {
    ANX_WORKSPACES: env.ANX_WORKSPACES,
    ANX_DEFAULT_WORKSPACE: env.ANX_DEFAULT_WORKSPACE,
    ANX_DEFAULT_ORGANIZATION: env.ANX_DEFAULT_ORGANIZATION,
  };
}

export function getWorkspaceHeader(headers) {
  const workspaceSlug = headers.get("x-anx-workspace-slug");
  if (workspaceSlug) {
    return workspaceSlug;
  }

  return null;
}

export function readOrganizationSlugHeader(headers) {
  return String(headers.get(ORGANIZATION_HEADER) ?? "").trim();
}

export function getOrganizationHeader(headers) {
  return readOrganizationSlugHeader(headers);
}

/**
 * Per-workspace localStorage key. The "legacy" name reflects older "project"
 * terminology in persisted keys, not environment variable compatibility.
 */
export function buildLegacyProjectStorageKey(baseKey, workspaceSlug) {
  const slug = normalizeWorkspaceSlug(workspaceSlug);
  if (!slug) {
    throw new Error("workspace slug is required for storage key");
  }
  return `${baseKey}:${slug}`;
}

export const WORKSPACE_HEADER_CONSTANTS = {
  ORGANIZATION_HEADER,
};
