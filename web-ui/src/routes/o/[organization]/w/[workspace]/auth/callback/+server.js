import { runWorkspaceAuthCallbackPost } from "$lib/server/workspaceAuthCallbackPost.js";

export async function POST(event) {
  return runWorkspaceAuthCallbackPost(event, {
    organizationSlug: event.params.organization,
    workspaceSlug: event.params.workspace,
  });
}
