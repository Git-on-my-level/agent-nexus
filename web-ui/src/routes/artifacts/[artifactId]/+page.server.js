import { redirectToDefaultWorkspace } from "$lib/server/workspaceRedirect";

export function load({ params }) {
  redirectToDefaultWorkspace(`/artifacts/${params.artifactId}`);
}
