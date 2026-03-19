import {
  DEFAULT_WORKSPACE_SLUG,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths";

const LEGACY_PROJECTS_ENV = "OAR_PROJECTS";
const LEGACY_DEFAULT_PROJECT_ENV = "OAR_DEFAULT_PROJECT";
const LEGACY_PROJECT_HEADER = "x-oar-project-slug";

export function resolveWorkspaceEnv(env) {
  const workspacesRaw = env.OAR_WORKSPACES ?? env[LEGACY_PROJECTS_ENV];
  const defaultWorkspaceRaw =
    env.OAR_DEFAULT_WORKSPACE ?? env[LEGACY_DEFAULT_PROJECT_ENV];

  return {
    OAR_WORKSPACES: workspacesRaw,
    OAR_DEFAULT_WORKSPACE: defaultWorkspaceRaw,
  };
}

export function getWorkspaceHeader(headers) {
  const workspaceSlug = headers.get("x-oar-workspace-slug");
  if (workspaceSlug) {
    return workspaceSlug;
  }

  const legacyProjectSlug = headers.get(LEGACY_PROJECT_HEADER);
  if (legacyProjectSlug) {
    return legacyProjectSlug;
  }

  return null;
}

export function buildLegacyProjectStorageKey(baseKey, workspaceSlug) {
  const slug = normalizeWorkspaceSlug(workspaceSlug) || DEFAULT_WORKSPACE_SLUG;
  return `${baseKey}:${slug}`;
}

export const LEGACY_CONSTANTS = {
  PROJECTS_ENV: LEGACY_PROJECTS_ENV,
  DEFAULT_PROJECT_ENV: LEGACY_DEFAULT_PROJECT_ENV,
  PROJECT_HEADER: LEGACY_PROJECT_HEADER,
};
