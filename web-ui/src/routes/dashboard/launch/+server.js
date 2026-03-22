import { json } from "@sveltejs/kit";

import {
  clearWorkspaceRefreshToken,
  writeWorkspaceAccessToken,
} from "$lib/server/authSession.js";
import {
  getControlClient,
  loadControlSession,
} from "$lib/server/controlSession.js";
import {
  appPath,
  normalizeAppPath,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths";

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
  const returnPath = String(body.return_path ?? "/").trim() || "/";

  if (!workspaceId) {
    return json(
      {
        error: {
          code: "invalid_request",
          message: "workspace_id is required.",
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
    const launchResponse = await client.createLaunchSession(workspaceId, {
      return_path: returnPath,
    });
    const launchSession = launchResponse?.launch_session;
    const exchangeToken = String(launchSession?.exchange_token ?? "").trim();
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
    const workspaceBasePath = normalizeAppPath(
      launchSession?.workspace_path ?? "",
    );
    const workspaceSlug = normalizeWorkspaceSlug(
      workspaceBasePath.split("/").filter(Boolean)[0],
    );
    if (!workspaceSlug) {
      return json(
        {
          error: {
            code: "launch_failed",
            message: "Control plane returned an invalid workspace path.",
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
      exchangeResponse?.grant?.bearer_token ?? "",
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

    const workspaceReturnPath = normalizeAppPath(
      launchSession?.return_path ?? returnPath,
    );
    return json({
      redirect_to: appPath(
        workspaceReturnPath === "/"
          ? workspaceBasePath
          : `${workspaceBasePath}${workspaceReturnPath}`,
      ),
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
