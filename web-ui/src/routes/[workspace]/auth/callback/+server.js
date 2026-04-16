import { env as privateEnv } from "$env/dynamic/private";
import { json, redirect } from "@sveltejs/kit";

import { normalizeBaseUrl } from "$lib/config.js";
import { sanitizeHostedReturnPath } from "$lib/hosted/launchFlow.js";
import {
  clearRetryableWorkspaceAuthFailureCount,
  writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken,
} from "$lib/server/authSession.js";
import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver.js";
import { workspacePath } from "$lib/workspacePaths.js";

function errorResponse(status, code, message) {
  return json(
    {
      error: {
        code,
        message,
      },
    },
    { status },
  );
}

async function readJSONPayload(response) {
  const text = String(await response.text().catch(() => "")).trim();
  if (!text) {
    return {};
  }
  try {
    return JSON.parse(text);
  } catch {
    return { message: text };
  }
}

async function postJSON(url, body) {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      accept: "application/json",
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
  const payload = await readJSONPayload(response);
  return {
    response,
    payload,
  };
}

export async function POST(event) {
  const resolved = await resolveWorkspaceBySlug({
    event,
    workspaceSlug: event.params.workspace,
  });
  if (resolved.error || !resolved.workspace) {
    return json(
      resolved.error?.payload ?? {
        error: {
          code: "workspace_not_found",
          message: "Workspace not found.",
        },
      },
      {
        status: resolved.error?.status ?? 404,
      },
    );
  }

  const workspaceSlug = resolved.workspaceSlug;
  const coreBaseURL = normalizeBaseUrl(resolved.workspace.coreBaseUrl);
  if (!coreBaseURL) {
    return errorResponse(
      503,
      "workspace_unavailable",
      "Workspace core endpoint is unavailable.",
    );
  }

  const controlBaseURL = normalizeBaseUrl(privateEnv.OAR_CONTROL_BASE_URL);
  if (!controlBaseURL) {
    return errorResponse(
      503,
      "control_plane_unavailable",
      "Control plane URL is not configured.",
    );
  }

  const form = await event.request.formData();
  const exchangeToken = String(form.get("exchange_token") ?? "").trim();
  const state = String(form.get("state") ?? "").trim();
  const formWorkspaceID = String(form.get("workspace_id") ?? "").trim();
  const returnPath = sanitizeHostedReturnPath(form.get("return_path") ?? "/");
  if (!exchangeToken || !state) {
    return errorResponse(
      400,
      "invalid_request",
      "exchange_token and state are required.",
    );
  }

  const resolvedWorkspaceID = String(
    resolved.workspace.workspaceId ?? resolved.workspace.id ?? "",
  ).trim();
  if (
    formWorkspaceID &&
    resolvedWorkspaceID &&
    formWorkspaceID !== resolvedWorkspaceID
  ) {
    return errorResponse(
      400,
      "invalid_request",
      "workspace_id does not match the callback workspace.",
    );
  }
  const workspaceID = formWorkspaceID || resolvedWorkspaceID;
  if (!workspaceID) {
    return errorResponse(
      400,
      "invalid_request",
      "workspace_id is required for session exchange.",
    );
  }

  const sessionExchangeURL = new URL(
    `/workspaces/${encodeURIComponent(workspaceID)}/session-exchange`,
    `${controlBaseURL}/`,
  ).toString();
  const exchanged = await postJSON(sessionExchangeURL, {
    exchange_token: exchangeToken,
    state,
  });
  if (!exchanged.response.ok) {
    return errorResponse(
      exchanged.response.status || 502,
      String(exchanged.payload?.error?.code ?? "session_exchange_failed"),
      String(
        exchanged.payload?.error?.message ??
          "Failed to exchange launch session with control plane.",
      ),
    );
  }

  const assertion = String(
    exchanged.payload?.grant?.bearer_token ?? "",
  ).trim();
  if (!assertion) {
    return errorResponse(
      502,
      "invalid_control_plane_response",
      "Control plane response did not include a workspace grant token.",
    );
  }

  const authTokenURL = new URL("/auth/token", `${coreBaseURL}/`).toString();
  const tokenExchange = await postJSON(authTokenURL, {
    grant_type: "workspace_human_grant",
    assertion,
  });
  if (!tokenExchange.response.ok) {
    return errorResponse(
      tokenExchange.response.status || 502,
      String(
        tokenExchange.payload?.error?.code ?? "workspace_token_exchange_failed",
      ),
      String(
        tokenExchange.payload?.error?.message ??
          "Workspace auth token exchange failed.",
      ),
    );
  }

  const tokens = tokenExchange.payload?.tokens ?? {};
  const refreshToken = String(tokens.refresh_token ?? "").trim();
  const accessToken = String(tokens.access_token ?? "").trim();
  if (!refreshToken || !accessToken) {
    return errorResponse(
      502,
      "invalid_workspace_response",
      "Workspace token exchange response was missing required tokens.",
    );
  }

  writeWorkspaceRefreshToken(event, workspaceSlug, refreshToken);
  writeWorkspaceAccessToken(event, workspaceSlug, accessToken);
  clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);

  throw redirect(303, workspacePath(workspaceSlug, returnPath));
}
