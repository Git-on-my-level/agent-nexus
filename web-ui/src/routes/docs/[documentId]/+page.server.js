import { redirectToDefaultWorkspace } from "$lib/server/workspaceRedirect";

export function load({ params }) {
  redirectToDefaultWorkspace(`/docs/${params.documentId}`);
}
