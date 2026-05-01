import { env as privateEnv } from "$env/dynamic/private";
import { json } from "@sveltejs/kit";

import { AuthErrorCode } from "$lib/authErrorCodes.js";
import {
  clearWorkspaceAuthSession,
  isRetryableWorkspaceAuthSessionError,
  isTerminalAccountSessionFailure,
  loadWorkspaceAuthenticatedAgent,
  resolveWorkspaceSlugFromEvent,
} from "$lib/server/authSession";
import {
  hostedWorkspaceCoreBaseUrl,
  hostedWorkspaceCoreProxyHeaders,
} from "$lib/server/hostedWorkspaceCore.js";
import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";

export async function GET(event) {
  const resolved = await resolveWorkspaceSlugFromEvent(event);
  if (resolved.error) {
    return json(resolved.error.payload, { status: resolved.error.status });
  }
  const provider =
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv);
  const hostedCoreBaseUrl =
    provider.mode === "hosted"
      ? hostedWorkspaceCoreBaseUrl({
          organizationSlug: resolved.organizationSlug,
          workspaceSlug: resolved.workspaceSlug,
        })
      : "";

  try {
    const agent = await loadWorkspaceAuthenticatedAgent({
      event,
      organizationSlug: resolved.organizationSlug,
      workspaceSlug: resolved.workspaceSlug,
      coreBaseUrl: hostedCoreBaseUrl || resolved.coreBaseUrl,
      headers: hostedCoreBaseUrl ? hostedWorkspaceCoreProxyHeaders(event) : {},
    });

    return json(
      {
        authenticated: Boolean(agent?.agent_id),
        agent: agent ?? null,
      },
      {
        headers: {
          "cache-control": "no-store",
        },
      },
    );
  } catch (error) {
    if (isRetryableWorkspaceAuthSessionError(error)) {
      return json(
        {
          authenticated: false,
          agent: null,
          retryable: true,
          error: {
            code: error.code,
            message: error.message,
          },
        },
        {
          headers: {
            "cache-control": "no-store",
          },
          status: 503,
        },
      );
    }
    if (isTerminalAccountSessionFailure(error)) {
      clearWorkspaceAuthSession(
        event,
        resolved.organizationSlug,
        resolved.workspaceSlug,
      );
      return json(
        {
          authenticated: false,
          agent: null,
          error: {
            code: AuthErrorCode.SESSION_ENDED_BY_ACCOUNT_STATUS,
            message:
              error?.details?.error?.message ??
              "Your organization session has ended. Sign in again.",
          },
        },
        {
          headers: {
            "cache-control": "no-store",
          },
          status: 401,
        },
      );
    }
    if (error?.status === 401 || error?.status === 403) {
      clearWorkspaceAuthSession(
        event,
        resolved.organizationSlug,
        resolved.workspaceSlug,
      );
      const upstreamCode = String(error?.details?.error?.code ?? "").trim();
      return json(
        {
          authenticated: false,
          agent: null,
          error: {
            code: upstreamCode || AuthErrorCode.AUTH_REQUIRED,
            message:
              error?.details?.error?.message ??
              error?.message ??
              "Authentication required.",
          },
        },
        {
          headers: {
            "cache-control": "no-store",
          },
          status: error.status,
        },
      );
    }
    return json(
      {
        authenticated: false,
        agent: null,
        error: {
          code: AuthErrorCode.CORE_UNREACHABLE,
          message: error?.message ?? "Workspace session request failed.",
        },
      },
      {
        headers: {
          "cache-control": "no-store",
        },
        status: 503,
      },
    );
  }
}

export async function DELETE(event) {
  const resolved = await resolveWorkspaceSlugFromEvent(event);
  if (resolved.error) {
    return json(resolved.error.payload, { status: resolved.error.status });
  }

  clearWorkspaceAuthSession(
    event,
    resolved.organizationSlug,
    resolved.workspaceSlug,
  );

  return json(
    {
      ok: true,
    },
    {
      headers: {
        "cache-control": "no-store",
      },
    },
  );
}

export const POST = DELETE;
