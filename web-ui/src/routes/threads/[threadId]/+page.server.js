import { redirectToDefaultWorkspace } from "$lib/server/workspaceRedirect";

export function load({ params, url }) {
  const pathname = `/threads/${params.threadId}${url.search}`;
  redirectToDefaultWorkspace(pathname);
}
