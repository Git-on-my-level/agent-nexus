import { redirectToDefaultWorkspace } from "$lib/server/workspaceRedirect";

export async function load(event) {
  await redirectToDefaultWorkspace(event, `/docs/${event.params.documentId}`);
}
