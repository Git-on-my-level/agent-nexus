import { env as privateEnv } from "$env/dynamic/private";
import { error, json } from "@sveltejs/kit";

import { AuthErrorCode } from "$lib/authErrorCodes.js";
import { logServerEvent } from "$lib/server/devLog.js";
import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import { runWorkspaceAuthCallbackPost } from "$lib/server/workspaceAuthCallbackPost.js";

function wantsJson(request) {
  return request.headers.get("accept")?.includes("application/json") ?? false;
}

function unresolvedReason(providerMode, lookupKind) {
  if (providerMode === "local") {
    return "control_plane_unavailable";
  }
  if (lookupKind === "unauthenticated") {
    return "control_plane_unauthenticated";
  }
  return "workspace_unknown";
}

/**
 * Control plane posts the launch finish form to `workspace.base_url + "/auth/callback"`.
 * When `base_url` is only the web UI origin (no `/o/{org}/w/{workspace}` prefix), the
 * POST hits this handler. We resolve org/workspace slugs via `workspace_id` and the
 * same exchange logic as the nested route.
 */
export async function POST(event) {
  const provider =
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv);
  const form = await event.request.formData();
  const workspaceId = String(form.get("workspace_id") ?? "").trim();
  let organizationSlug = "";
  let workspaceSlug = "";
  let lookupKind = "missing";

  if (workspaceId) {
    const lookup = await provider.resolveWorkspaceById({ event, workspaceId });
    lookupKind = lookup.kind;
    if (lookup.kind === "found") {
      organizationSlug = lookup.workspace.organizationSlug;
      workspaceSlug = lookup.workspace.slug;
    }
  }

  if (!organizationSlug || !workspaceSlug) {
    const reason = unresolvedReason(provider.mode, lookupKind);
    logServerEvent(
      "auth.callback.root.workspace_resolve_failed",
      {
        workspace_id: workspaceId || "(empty)",
        mode: provider.mode,
        reason,
      },
      { level: "warn" },
    );
    const msg =
      "Could not resolve workspace for this callback. Ensure the workspace base URL in the control plane includes the full shell path (/o/{org}/w/{workspace}), or configure ANX_CONTROL_BASE_URL plus ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN (or oar_cp_dev_access_token) so the server can look up the workspace by id.";
    if (wantsJson(event.request)) {
      return json(
        {
          error: {
            code: AuthErrorCode.WORKSPACE_RESOLVE_FAILED,
            message: msg,
            reason,
            mode: provider.mode,
          },
        },
        { status: 400 },
      );
    }
    throw error(400, {
      message: msg,
      code: AuthErrorCode.WORKSPACE_RESOLVE_FAILED,
    });
  }

  return runWorkspaceAuthCallbackPost(
    event,
    { organizationSlug, workspaceSlug },
    form,
  );
}
