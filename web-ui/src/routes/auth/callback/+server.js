import { env as privateEnv } from "$env/dynamic/private";
import { error, json } from "@sveltejs/kit";

import { AuthErrorCode } from "$lib/authErrorCodes.js";
import { resolveAuthCapabilities } from "$lib/server/authCapabilities.js";
import { fetchWorkspaceByIdFromControlPlane } from "$lib/server/controlPlaneWorkspace.js";
import { logServerEvent } from "$lib/server/devLog.js";
import { runWorkspaceAuthCallbackPost } from "$lib/server/workspaceAuthCallbackPost.js";

function wantsJson(request) {
  return request.headers.get("accept")?.includes("application/json") ?? false;
}

/**
 * Control plane posts the launch finish form to `workspace.base_url + "/auth/callback"`.
 * When `base_url` is only the web UI origin (no `/o/{org}/w/{workspace}` prefix), the
 * POST hits this handler. We resolve org/workspace slugs via `workspace_id` and the
 * same exchange logic as the nested route.
 */
export async function POST(event) {
  const caps = resolveAuthCapabilities(privateEnv, event);
  const form = await event.request.formData();
  const workspaceId = String(form.get("workspace_id") ?? "").trim();
  let organizationSlug = "";
  let workspaceSlug = "";

  if (workspaceId && caps.supportsCpWorkspaceIdLookup) {
    const entry = await fetchWorkspaceByIdFromControlPlane({
      env: privateEnv,
      workspaceId,
      fetchFn: event.fetch,
      getCookie: event.cookies.get.bind(event.cookies),
    });
    if (entry?.organizationSlug && entry?.slug) {
      organizationSlug = entry.organizationSlug;
      workspaceSlug = entry.slug;
    }
  }

  if (!organizationSlug || !workspaceSlug) {
    const reason =
      caps.mode === "local"
        ? "local_mode_no_control_plane"
        : caps.mode === "hosted"
          ? "hosted_mode_workspace_id_lookup_disabled"
          : "lookup_failed_or_missing_token";
    logServerEvent(
      "auth.callback.root.workspace_resolve_failed",
      {
        workspace_id: workspaceId || "(empty)",
        mode: caps.mode,
        reason,
      },
      { level: "warn" },
    );
    const msg =
      "Could not resolve workspace for this callback. Ensure the workspace base URL in the control plane includes the full shell path (/o/{org}/w/{workspace}), or configure ANX_SAAS_PACKED_HOST_DEV, ANX_CONTROL_BASE_URL, and ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN (or oar_cp_dev_access_token) so the server can look up the workspace by id.";
    if (wantsJson(event.request)) {
      return json(
        {
          error: {
            code: AuthErrorCode.WORKSPACE_RESOLVE_FAILED,
            message: msg,
            reason,
            mode: caps.mode,
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
