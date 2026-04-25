import { env as privateEnv } from "$env/dynamic/private";
import { redirect } from "@sveltejs/kit";

import { sanitizeHostedReturnPath } from "$lib/hosted/launchFlow.js";
import { loadWorkspaceAuthenticatedAgent } from "$lib/server/authSession";
import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import { handleLaunchInstruction } from "$lib/server/outOfWorkspace/launchSession.js";
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver";
import { workspacePath } from "$lib/workspacePaths";

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
      workspaceSlug: resolved.workspaceSlug,
      coreBaseUrl: workspace.coreBaseUrl,
    });
  } catch (error) {
    if (error?.status) {
      return;
    }
    throw error;
  }

  if (agent?.agent_id) {
    const returnTo = sanitizeHostedReturnPath(
      event.url.searchParams.get("return_to") ??
        event.url.searchParams.get("return_path") ??
        "/",
    );
    throw redirect(
      307,
      workspacePath(
        resolved.organizationSlug,
        resolved.workspaceSlug,
        returnTo,
      ),
    );
  }

  if (provider.mode !== "hosted") {
    return;
  }

  const workspaceID = String(
    workspace.workspaceId ?? workspace.id ?? "",
  ).trim();
  const returnPath = sanitizeHostedReturnPath(
    event.url.searchParams.get("return_path") ??
      event.url.searchParams.get("return_to") ??
      "/",
  );

  const instruction = await provider.beginLaunchSession({
    event,
    workspaceId: workspaceID,
    workspaceSlug: resolved.workspaceSlug,
    returnPath,
  });
  handleLaunchInstruction(instruction);
}
