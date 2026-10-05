import { env as privateEnv } from "$env/dynamic/private";

import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import {
  toPublicWorkspaceCatalog,
  workspaceSessionProbe,
} from "$lib/server/workspaceCatalog";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";

export async function load(event) {
  const provider =
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv);
  const capabilities = provider.describeShellCapabilities();
  return {
    ...toPublicWorkspaceCatalog(await resolveWorkspaceCatalog(event), {
      hasSession: workspaceSessionProbe(event, capabilities),
    }),
    shellCapabilities: capabilities,
  };
}
