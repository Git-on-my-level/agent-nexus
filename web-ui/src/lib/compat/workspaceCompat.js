import { normalizeWorkspaceSlug } from "$lib/workspacePaths";

const LEGACY_PROJECTS_ENV = "ANX_PROJECTS";
const LEGACY_DEFAULT_PROJECT_ENV = "ANX_DEFAULT_PROJECT";
const LEGACY_PROJECT_HEADER = "x-oar-project-slug";
const ORGANIZATION_HEADER = "x-anx-organization-slug";

export function resolveWorkspaceEnv(env) {
  const workspacesRaw = env.ANX_WORKSPACES ?? env[LEGACY_PROJECTS_ENV];
  const defaultWorkspaceRaw =
    env.ANX_DEFAULT_WORKSPACE ?? env[LEGACY_DEFAULT_PROJECT_ENV];

  return {
    ANX_WORKSPACES: workspacesRaw,
    ANX_DEFAULT_WORKSPACE: defaultWorkspaceRaw,
    ANX_DEFAULT_ORGANIZATION: env.ANX_DEFAULT_ORGANIZATION,
  };
}

export function getWorkspaceHeader(headers) {
  const workspaceSlug = headers.get("x-anx-workspace-slug");
  if (workspaceSlug) {
    return workspaceSlug;
  }

  const legacyProjectSlug = headers.get(LEGACY_PROJECT_HEADER);
  if (legacyProjectSlug) {
    return legacyProjectSlug;
  }

  return null;
}

export function readOrganizationSlugHeader(headers) {
  return String(headers.get(ORGANIZATION_HEADER) ?? "").trim();
}

export function getOrganizationHeader(headers) {
  return readOrganizationSlugHeader(headers);
}

export function buildLegacyProjectStorageKey(baseKey, workspaceSlug) {
  const slug = normalizeWorkspaceSlug(workspaceSlug);
  if (!slug) {
    throw new Error("workspace slug is required for storage key");
  }
  return `${baseKey}:${slug}`;
}

export const LEGACY_CONSTANTS = {
  PROJECTS_ENV: LEGACY_PROJECTS_ENV,
  DEFAULT_PROJECT_ENV: LEGACY_DEFAULT_PROJECT_ENV,
  PROJECT_HEADER: LEGACY_PROJECT_HEADER,
  ORGANIZATION_HEADER,
};
