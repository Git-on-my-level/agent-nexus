import { env as privateEnv } from "$env/dynamic/private";
import { redirect } from "@sveltejs/kit";

import { normalizeBaseUrl } from "$lib/config.js";
import {
  buildHostedSignInPath,
  normalizeHostedLaunchFinishURL,
  sanitizeHostedReturnPath,
} from "$lib/hosted/launchFlow.js";
import { loadWorkspaceAuthenticatedAgent } from "$lib/server/authSession";
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver";
import { workspacePath } from "$lib/workspacePaths";

async function tryHostedLaunchFinishUrl({
  fetchFn,
  controlBaseUrl,
  workspaceID,
  accessToken,
  returnPath,
}) {
  const base = normalizeBaseUrl(controlBaseUrl);
  const token = String(accessToken ?? "").trim();
  const wsId = String(workspaceID ?? "").trim();
  const doFetch = typeof fetchFn === "function" ? fetchFn : fetch;
  if (!base || !token || !wsId) {
    return "";
  }
  const url = `${base}/workspaces/${encodeURIComponent(wsId)}/launch-sessions`;
  let response;
  try {
    response = await doFetch(url, {
      method: "POST",
      headers: {
        accept: "application/json",
        "content-type": "application/json",
        authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ return_path: returnPath }),
    });
  } catch {
    return "";
  }
  if (!response.ok) {
    return "";
  }
  let payload;
  try {
    payload = await response.json();
  } catch {
    return "";
  }
  const finishRaw = String(payload?.launch_session?.finish_url ?? "").trim();
  return normalizeHostedLaunchFinishURL(finishRaw);
}

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
  const workspaceID = String(
    workspace?.workspaceId ?? workspace?.id ?? "",
  ).trim();
  return workspaceID !== "";
}

export async function load(event) {
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

  const authModeLookup = await loadWorkspaceHumanAuthMode(
    event,
    workspace.coreBaseUrl,
  );
  const hostedWorkspace = isHostedWorkspaceContext(workspace);
  const failClosedToHostedSSO =
    hostedWorkspace &&
    (!authModeLookup.available ||
      authModeLookup.mode === "external_grant" ||
      authModeLookup.mode === "");
  if (!failClosedToHostedSSO) {
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

  const controlBaseUrl = normalizeBaseUrl(
    privateEnv.ANX_CONTROL_BASE_URL ?? "",
  );
  const cpAccessToken = String(
    event.cookies.get("oar_cp_dev_access_token") ?? "",
  ).trim();
  if (controlBaseUrl && cpAccessToken) {
    const finishUrl = await tryHostedLaunchFinishUrl({
      fetchFn: event.fetch,
      controlBaseUrl,
      workspaceID,
      accessToken: cpAccessToken,
      returnPath,
    });
    if (finishUrl) {
      throw redirect(303, finishUrl);
    }
  }

  throw redirect(
    307,
    buildHostedSignInPath({
      workspaceSlug: resolved.workspaceSlug,
      workspaceId: workspaceID,
      returnPath,
    }),
  );
}
