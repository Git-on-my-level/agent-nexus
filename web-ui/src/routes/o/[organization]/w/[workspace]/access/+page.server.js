import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import { resolveCliBaseUrl } from "$lib/server/cliBaseUrl.js";
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver";

export async function load(event) {
  const provider = event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider();
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: event.params.organization,
    workspaceSlug: event.params.workspace,
  });
  return {
    coreBaseUrl: resolved.workspace?.coreBaseUrl ?? "",
    workspaceId:
      resolved.workspace?.workspaceId ?? resolved.workspace?.id ?? "",
    cliBaseUrl: resolveCliBaseUrl(event, resolved),
    outOfWorkspaceMode: provider.mode,
  };
}
