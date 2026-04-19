import { LEGACY_CONSTANTS } from "$lib/compat/workspaceCompat";
import {
  APP_BASE_PATH,
  WORKSPACE_HEADER,
  parseWorkspaceRouteSlugs,
} from "$lib/workspacePaths";

export function buildCoreRequestContextHeaders({
  storeOrg,
  storeWorkspace,
  pathname = "/",
  basePath = APP_BASE_PATH,
} = {}) {
  let org = String(storeOrg ?? "").trim();
  let workspace = String(storeWorkspace ?? "").trim();
  if (!org || !workspace) {
    const fromUrl = parseWorkspaceRouteSlugs(pathname, basePath);
    if (!org) {
      org = String(fromUrl.organizationSlug ?? "").trim();
    }
    if (!workspace) {
      workspace = String(fromUrl.workspaceSlug ?? "").trim();
    }
  }

  const headers = {};
  if (workspace) {
    headers[WORKSPACE_HEADER] = workspace;
  }
  if (org) {
    headers[LEGACY_CONSTANTS.ORGANIZATION_HEADER] = org;
  }
  return headers;
}
