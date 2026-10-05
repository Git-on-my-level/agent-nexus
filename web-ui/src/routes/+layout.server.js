import { env as privateEnv } from "$env/dynamic/private";

import { readWorkspaceRefreshToken } from "$lib/server/authSession";
import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";

export async function load(event) {
  const provider =
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv);
  const capabilities = provider.describeShellCapabilities();
  /*
   * Only hosted scopes a session per workspace. A self-hosted shell talks to
   * one core with one identity, so every workspace in its catalog is readable
   * and the question does not arise.
   */
  const hasSession =
    capabilities.mode === "hosted"
      ? (organizationSlug, workspaceSlug) =>
          Boolean(
            readWorkspaceRefreshToken(event, organizationSlug, workspaceSlug),
          )
      : undefined;
  return {
    ...toPublicWorkspaceCatalog(await resolveWorkspaceCatalog(event), {
      hasSession,
    }),
    shellCapabilities: capabilities,
  };
}
