import { redirectToDefaultWorkspace } from "$lib/server/workspaceRedirect";

export async function load(event) {
  const pathname = `/topics/${event.params.threadId}${event.url.search}`;
  await redirectToDefaultWorkspace(event, pathname);
}
