import { env as privateEnv } from "$env/dynamic/private";
import { env as publicEnv } from "$env/dynamic/public";

import { isHostedWebUiShell } from "$lib/server/controlPlaneWorkspace.js";
import { toPublicWorkspaceCatalog } from "$lib/server/workspaceCatalog";
import { resolveWorkspaceCatalog } from "$lib/server/workspaceResolver";

export async function load(event) {
  return {
    ...toPublicWorkspaceCatalog(await resolveWorkspaceCatalog(event)),
    hostedMode: isHostedWebUiShell(privateEnv),
    hostedAccountPath:
      String(publicEnv.PUBLIC_ANX_HOSTED_ACCOUNT_PATH ?? "").trim() ||
      "/hosted/onboarding",
  };
}
