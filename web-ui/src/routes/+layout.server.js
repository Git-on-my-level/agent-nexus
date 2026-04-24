import { env as privateEnv } from "$env/dynamic/private";

import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";

export async function load(event) {
  const provider =
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv);
  return {
    ...toPublicWorkspaceCatalog(await resolveWorkspaceCatalog(event)),
    shellCapabilities: provider.describeShellCapabilities(),
  };
}
