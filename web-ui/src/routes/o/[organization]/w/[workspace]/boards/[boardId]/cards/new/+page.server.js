import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver";

export async function load(event) {
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: event.params.organization,
    workspaceSlug: event.params.workspace,
  });

  return {
    workspaceId:
      resolved.workspace?.workspaceId ?? resolved.workspace?.id ?? "",
  };
}
