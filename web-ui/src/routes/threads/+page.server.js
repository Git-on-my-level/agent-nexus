import { redirectToDefaultWorkspace } from "$lib/server/workspaceRedirect";

export function load() {
  redirectToDefaultWorkspace("/threads");
}
