import { buildCoreWorkspaceRoutingHeadersFromSlugs } from "$lib/coreWorkspaceHeadersShared";
import { APP_BASE_PATH, parseWorkspaceRouteSlugs } from "$lib/workspacePaths";

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

  return buildCoreWorkspaceRoutingHeadersFromSlugs({
    organizationSlug: org,
    workspaceSlug: workspace,
  });
}
