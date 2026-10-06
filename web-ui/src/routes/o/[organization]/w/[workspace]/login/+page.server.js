import { env as privateEnv } from "$env/dynamic/private";
import { redirect } from "@sveltejs/kit";

import {
  confineWorkspaceReturnPath,
  sanitizeReturnPath,
} from "$lib/workspaceLaunchFlow.js";
import { loadWorkspaceAuthenticatedAgent } from "$lib/server/authSession";
import {
  hostedWorkspaceCoreBaseUrl,
  hostedWorkspaceCoreProxyHeaders,
} from "$lib/server/hostedWorkspaceCore.js";
import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import { handleLaunchInstruction } from "$lib/server/outOfWorkspace/launchSession.js";
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver";

export async function load(event) {
  const provider =
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv);
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: event.params.organization,
    workspaceSlug: event.params.workspace,
  });
  const workspace = resolved.workspace;

  if (!workspace || resolved.error) {
    return;
  }

  let agent;
  try {
    agent = await loadWorkspaceAuthenticatedAgent({
      event,
      organizationSlug: resolved.organizationSlug,
      workspaceSlug: resolved.workspaceSlug,
      coreBaseUrl:
        provider.mode === "hosted"
          ? hostedWorkspaceCoreBaseUrl({
              organizationSlug: resolved.organizationSlug,
              workspaceSlug: resolved.workspaceSlug,
            })
          : workspace.coreBaseUrl,
      headers:
        provider.mode === "hosted"
          ? hostedWorkspaceCoreProxyHeaders(event)
          : {},
    });
  } catch (error) {
    if (error?.status) {
      return;
    }
    throw error;
  }

  if (agent?.agent_id) {
    throw redirect(
      307,
      confineWorkspaceReturnPath({
        origin: event.url?.origin,
        organizationSlug: resolved.organizationSlug,
        workspaceSlug: resolved.workspaceSlug,
        returnPath:
          event.url.searchParams.get("return_to") ??
          event.url.searchParams.get("return_path") ??
          "/",
      }),
    );
  }

  if (provider.mode !== "hosted") {
    return;
  }

  const workspaceID = String(
    workspace.workspaceId ?? workspace.id ?? "",
  ).trim();
  const returnPath = sanitizeReturnPath(
    event.url.searchParams.get("return_path") ??
      event.url.searchParams.get("return_to") ??
      "/",
  );

  const instruction = await provider.beginLaunchSession({
    event,
    workspaceId: workspaceID,
    organizationSlug: resolved.organizationSlug,
    workspaceSlug: resolved.workspaceSlug,
    returnPath,
  });
  handleLaunchInstruction(instruction);
}
