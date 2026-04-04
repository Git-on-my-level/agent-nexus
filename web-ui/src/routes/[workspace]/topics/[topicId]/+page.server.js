import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver";

export async function load(event) {
  const resolved = await resolveWorkspaceBySlug({
    event,
    workspaceSlug: event.params.workspace,
  });

  return {
    workspaceId:
      resolved.workspace?.workspaceId ?? resolved.workspace?.id ?? "",
  };
}
