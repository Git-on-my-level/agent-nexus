import { redirectToRecentWorkspaceOrChooser } from "$lib/server/workspaceRedirect";

export async function load(event) {
  await redirectToRecentWorkspaceOrChooser(event);
}
