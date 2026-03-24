import { redirectToDefaultWorkspace } from "$lib/server/workspaceRedirect";

export async function load(event) {
  const pathname = `/threads/${event.params.threadId}${event.url.search}`;
  await redirectToDefaultWorkspace(event, pathname);
}
