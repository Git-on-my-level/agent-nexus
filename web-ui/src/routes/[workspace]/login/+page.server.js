import { redirect } from "@sveltejs/kit";

import {
  buildHostedSignInPath,
  sanitizeHostedReturnPath,
} from "$lib/hosted/launchFlow.js";
import { loadWorkspaceAuthenticatedAgent } from "$lib/server/authSession";
import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver";
import { workspacePath } from "$lib/workspacePaths";

async function loadWorkspaceHumanAuthMode(event, coreBaseUrl) {
  if (!coreBaseUrl) {
    return { available: false, mode: "" };
  }
  const handshakeURL = new URL("/meta/handshake", `${coreBaseUrl}/`).toString();
  try {
    const response = await event.fetch(handshakeURL, {
      headers: {
        accept: "application/json",
      },
    });
    if (!response.ok) {
      return { available: false, mode: "" };
    }
    const payload = await response.json();
    return {
      available: true,
      mode: String(payload?.human_auth_mode ?? "").trim(),
    };
  } catch {
    return { available: false, mode: "" };
  }
}

function isHostedWorkspaceContext(workspace) {
  const workspaceID = String(workspace?.workspaceId ?? workspace?.id ?? "").trim();
  return workspaceID !== "";
}

export async function load(event) {
  const resolved = await resolveWorkspaceBySlug({
    event,
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
    throw redirect(307, workspacePath(resolved.workspaceSlug));
  }

  const authModeLookup = await loadWorkspaceHumanAuthMode(
    event,
    workspace.coreBaseUrl,
  );
  const hostedWorkspace = isHostedWorkspaceContext(workspace);
  const failClosedToHostedSSO =
    hostedWorkspace &&
    (!authModeLookup.available || authModeLookup.mode === "external_grant" || authModeLookup.mode === "");
  if (!failClosedToHostedSSO) {
    return;
  }

  const workspaceID = String(workspace.workspaceId ?? workspace.id ?? "").trim();
  const returnPath = sanitizeHostedReturnPath(
    event.url.searchParams.get("return_path") ??
      event.url.searchParams.get("return_to") ??
      "/",
  );
  throw redirect(
    307,
    buildHostedSignInPath({
      workspaceSlug: resolved.workspaceSlug,
      workspaceId: workspaceID,
      returnPath,
    }),
  );
}
