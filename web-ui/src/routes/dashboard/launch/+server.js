import { json } from "@sveltejs/kit";

import {
  clearWorkspaceRefreshToken,
  writeWorkspaceAccessToken,
} from "$lib/server/authSession.js";
import {
  getControlClient,
  loadControlSession,
} from "$lib/server/controlSession.js";
import { normalizeWorkspaceSlug, workspacePath } from "$lib/workspacePaths";

export async function POST(event) {
  const session = await loadControlSession(event);
  if (!session?.accessToken) {
    return json(
      {
        error: {
          code: "unauthorized",
          message: "Control session is required.",
        },
      },
      { status: 401 },
    );
  }

  const body = await event.request.json().catch(() => ({}));
  const workspaceId = String(body.workspace_id ?? "").trim();
  const workspaceSlug = normalizeWorkspaceSlug(body.workspace_slug);
  const returnPath = String(body.return_path ?? "/").trim() || "/";

  if (!workspaceId || !workspaceSlug) {
    return json(
      {
        error: {
          code: "invalid_request",
          message: "workspace_id and workspace_slug are required.",
        },
      },
      { status: 400 },
    );
  }
  if (!returnPath.startsWith("/")) {
    return json(
      {
        error: {
          code: "invalid_request",
          message: "return_path must start with '/'.",
        },
      },
      { status: 400 },
    );
  }

  try {
    const client = getControlClient(event);
    const workspaceResponse = await client.getWorkspace(workspaceId);
    const workspace = workspaceResponse.workspace ?? workspaceResponse;
    const resolvedWorkspaceSlug = normalizeWorkspaceSlug(workspace?.slug);
    if (!resolvedWorkspaceSlug || resolvedWorkspaceSlug !== workspaceSlug) {
      return json(
        {
          error: {
            code: "invalid_request",
            message: "workspace_id does not match workspace_slug.",
          },
        },
        { status: 400 },
      );
    }

    const launchResponse = await client.createLaunchSession(workspaceId, {
      return_path: returnPath,
    });
    const exchangeToken = String(
      launchResponse?.launch_session?.exchange_token ??
        launchResponse?.exchange_token ??
        "",
    ).trim();
    if (!exchangeToken) {
      return json(
        {
          error: {
            code: "launch_failed",
            message: "Control plane returned an empty exchange token.",
          },
        },
        { status: 502 },
      );
    }

    const exchangeResponse = await client.exchangeWorkspaceSession(
      workspaceId,
      exchangeToken,
    );
    const accessToken = String(
      exchangeResponse?.grant?.bearer_token ??
        exchangeResponse?.bearer_token ??
        exchangeResponse?.access_token ??
        "",
    ).trim();
    if (!accessToken) {
      return json(
        {
          error: {
            code: "launch_failed",
            message: "Control plane returned an empty workspace grant token.",
          },
        },
        { status: 502 },
      );
    }

    clearWorkspaceRefreshToken(event, workspaceSlug);
    writeWorkspaceAccessToken(event, workspaceSlug, accessToken);

    return json({
      redirect_to: workspacePath(workspaceSlug, returnPath),
    });
  } catch (error) {
    return json(
      {
        error: {
          code: "launch_failed",
          message:
            error instanceof Error
              ? error.message
              : "Failed to launch workspace.",
        },
      },
      { status: error?.status ?? 502 },
    );
  }
}
