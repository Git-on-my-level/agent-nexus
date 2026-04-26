import { error, json, redirect } from "@sveltejs/kit";

import { normalizeBaseUrl } from "$lib/config.js";
import {
  CALLBACK_CODES_UNKNOWN_COPY,
  CALLBACK_CODES_WITH_TABLE_COPY,
  CALLBACK_COPY,
} from "$lib/hosted/callbackErrorCopy.js";
import { sanitizeHostedReturnPath } from "$lib/hosted/launchFlow.js";
import {
  clearRetryableWorkspaceAuthFailureCount,
  writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken,
} from "$lib/server/authSession.js";
import { coreBaseUrlForNodeFetch } from "$lib/server/coreBaseUrlForNodeFetch.js";
import { coreEndpointURL } from "$lib/server/coreEndpoint.js";
import { buildServerCoreWorkspaceContextHeaders } from "$lib/server/coreWorkspaceContextHeaders.js";
import { logServerEvent } from "$lib/server/devLog";
import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver.js";
import { workspacePath } from "$lib/workspacePaths.js";

function wantsJson(request) {
  return request.headers.get("accept")?.includes("application/json") ?? false;
}

function userFacingBody(code, technicalMessage) {
  if (code === "state_mismatch") {
    return CALLBACK_COPY.UNKNOWN.body;
  }
  if (CALLBACK_CODES_WITH_TABLE_COPY.has(code)) {
    return /** @type {{ body: string }} */ (CALLBACK_COPY[code]).body;
  }
  if (CALLBACK_CODES_UNKNOWN_COPY.has(code)) {
    return CALLBACK_COPY.UNKNOWN.body;
  }
  return technicalMessage;
}

/**
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {number} status
 * @param {string} canonicalCode
 * @param {string} technicalMessage
 * @param {{ workspace_name?: string | null }} [options]
 */
function respondWithCallbackError(
  event,
  status,
  canonicalCode,
  technicalMessage,
  options = {},
) {
  const workspaceName = options.workspace_name;
  const errorObj = {
    code: canonicalCode,
    message: technicalMessage,
  };
  if (workspaceName != null && String(workspaceName).trim() !== "") {
    errorObj.workspace_name = String(workspaceName).trim();
  }

  if (wantsJson(event.request)) {
    return json({ error: errorObj }, { status });
  }

  throw error(status, {
    message: userFacingBody(canonicalCode, technicalMessage),
    code: canonicalCode,
    workspace_name: workspaceName != null ? String(workspaceName).trim() : null,
  });
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

function fetchErrorDetail(err) {
  if (!err || typeof err !== "object") {
    return String(err ?? "fetch failed");
  }
  const cause = /** @type {{ code?: string, message?: string }} */ (err.cause);
  if (cause && typeof cause === "object") {
    const code = String(cause.code ?? "").trim();
    const msg = String(cause.message ?? "").trim();
    if (code && msg) {
      return `${code}: ${msg}`;
    }
    if (msg) {
      return msg;
    }
    if (code) {
      return code;
    }
  }
  const message = String(err.message ?? "").trim();
  return message || "fetch failed";
}

async function postJSON(url, body, options = {}) {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      accept: "application/json",
      "content-type": "application/json",
      ...(options.headers ?? {}),
    },
    body: JSON.stringify(body),
  });
  const payload = await readJSONPayload(response);
  return {
    response,
    payload,
  };
}

async function postJSONWithNetworkRetries(url, body, options) {
  const attempts = Math.max(1, Number(options?.attempts ?? 8));
  const delayMs = Math.max(50, Number(options?.delayMs ?? 400));
  let lastErr = /** @type {unknown} */ (null);
  for (let i = 0; i < attempts; i++) {
    try {
      return await postJSON(url, body, options);
    } catch (err) {
      lastErr = err;
      if (i + 1 < attempts) {
        await new Promise((r) => setTimeout(r, delayMs));
      }
    }
  }
  throw lastErr;
}

function workspaceDisplayName(resolved) {
  const w = resolved?.workspace;
  if (!w) {
    return null;
  }
  const label = String(w.label ?? "").trim();
  if (label) {
    return label;
  }
  const slug = String(w.slug ?? "").trim();
  return slug || null;
}

/**
 * Shared workspace launch OAuth callback (POST). Used by nested
 * `/o/{org}/w/{ws}/auth/callback` and root `/auth/callback` when the control
 * plane posts to `workspace.base_url + "/auth/callback"` with only an origin
 * path (no `/o/.../w/...` prefix).
 *
 * **Hosted workspace switches:** this path only writes **workspace-scoped**
 * cookies (`anx_ui_session_*` / `anx_ui_access_*`). It does **not** clear the
 * control-plane session cookies (`anx_cp_access_token` in production,
 * `anx_cp_dev_access_token` in local dev), so switching between hosted
 * workspaces reuses the CP session as intended.
 *
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {{ organizationSlug: string, workspaceSlug: string }} routeParams
 * @param {FormData} [formOverride] When set (e.g. root `/auth/callback`), avoids re-reading a consumed request body.
 */
export async function runWorkspaceAuthCallbackPost(
  event,
  routeParams,
  formOverride,
) {
  const eventPath = String(
    event.url?.pathname ?? event.request?.url ?? "",
  ).trim();
  const provider = event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider();
  const form = formOverride ?? (await event.request.formData());
  const formWorkspaceID = String(form.get("workspace_id") ?? "").trim();
  logServerEvent("auth.callback.start", {
    path: eventPath,
    organization: routeParams.organizationSlug,
    workspace: routeParams.workspaceSlug,
    workspace_id: formWorkspaceID,
    has_exchange_token: String(form.get("exchange_token") ?? "").trim() !== "",
    has_state: String(form.get("state") ?? "").trim() !== "",
  });
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: routeParams.organizationSlug,
    workspaceSlug: routeParams.workspaceSlug,
  });
  const workspaceNameEarly = workspaceDisplayName(resolved);

  let resolvedWorkspace = resolved.workspace ?? null;
  let resolvedOrganizationSlug = resolved.organizationSlug ?? "";
  let resolvedWorkspaceSlug = resolved.workspaceSlug ?? "";

  if ((resolved.error || !resolvedWorkspace) && formWorkspaceID) {
    const fallback = await provider.resolveWorkspaceById({
      event,
      workspaceId: formWorkspaceID,
    });
    if (fallback.kind === "found" && fallback.workspace) {
      const fallbackOrg = String(
        fallback.workspace.organizationSlug ?? "",
      ).trim();
      const fallbackSlug = String(fallback.workspace.slug ?? "").trim();
      const routeOrg = String(routeParams.organizationSlug ?? "").trim();
      const routeSlug = String(routeParams.workspaceSlug ?? "").trim();
      if (
        (!routeOrg || routeOrg === fallbackOrg) &&
        (!routeSlug || routeSlug === fallbackSlug)
      ) {
        resolvedWorkspace = fallback.workspace;
        resolvedOrganizationSlug = fallbackOrg;
        resolvedWorkspaceSlug = fallbackSlug;
      }
    }
  }

  if (!resolvedWorkspace) {
    logServerEvent(
      "auth.callback.workspace_unresolved",
      {
        path: eventPath,
        organization: routeParams.organizationSlug,
        workspace: routeParams.workspaceSlug,
        workspace_id: formWorkspaceID,
        status: resolved.error?.status ?? 404,
        code: resolved.error?.payload?.error?.code ?? "workspace_not_found",
      },
      { level: "warn" },
    );
    if (wantsJson(event.request)) {
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
    throw error(resolved.error?.status ?? 404, {
      message:
        resolved.error?.payload?.error?.message ?? "Workspace not found.",
      code: "workspace_not_found",
      workspace_name: workspaceNameEarly,
    });
  }

  const workspaceSlug = resolvedWorkspaceSlug;
  const coreBaseURL = coreBaseUrlForNodeFetch(
    normalizeBaseUrl(resolvedWorkspace.coreBaseUrl),
  );
  if (!coreBaseURL) {
    logServerEvent(
      "auth.callback.core_unavailable",
      {
        path: eventPath,
        workspace: resolvedWorkspaceSlug,
      },
      { level: "warn" },
    );
    return respondWithCallbackError(
      event,
      503,
      "workspace_unavailable",
      "Workspace core endpoint is unavailable.",
      { workspace_name: workspaceNameEarly },
    );
  }

  const exchangeToken = String(form.get("exchange_token") ?? "").trim();
  const state = String(form.get("state") ?? "").trim();
  const returnPath = sanitizeHostedReturnPath(form.get("return_path") ?? "/");
  if (!exchangeToken || !state) {
    return respondWithCallbackError(
      event,
      400,
      "invalid_request",
      "exchange_token and state are required.",
      { workspace_name: workspaceNameEarly },
    );
  }

  const resolvedWorkspaceID = String(
    resolvedWorkspace.workspaceId ?? resolvedWorkspace.id ?? "",
  ).trim();
  if (
    formWorkspaceID &&
    resolvedWorkspaceID &&
    formWorkspaceID !== resolvedWorkspaceID
  ) {
    return respondWithCallbackError(
      event,
      400,
      "invalid_request",
      "workspace_id does not match the callback workspace.",
      { workspace_name: workspaceNameEarly },
    );
  }
  const workspaceID = formWorkspaceID || resolvedWorkspaceID;
  if (!workspaceID) {
    return respondWithCallbackError(
      event,
      400,
      "invalid_request",
      "workspace_id is required for session exchange.",
      { workspace_name: workspaceNameEarly },
    );
  }

  const workspaceName = workspaceNameEarly;
  const exchanged = await provider.exchangeLaunchSession({
    event,
    request: {
      workspaceId: workspaceID,
      exchangeToken,
      state,
    },
  });
  if (!exchanged.ok) {
    logServerEvent(
      "auth.callback.exchange_failed",
      {
        path: eventPath,
        workspace_id: workspaceID,
        status: exchanged.status || 502,
        code: exchanged.code || "session_exchange_failed",
      },
      { level: "warn" },
    );
    return respondWithCallbackError(
      event,
      exchanged.status || 502,
      exchanged.code || "session_exchange_failed",
      exchanged.message ||
        "Failed to exchange launch session with control plane.",
      { workspace_name: workspaceName },
    );
  }
  const assertion = exchanged.assertion;
  logServerEvent("auth.callback.exchange_ok", {
    path: eventPath,
    workspace_id: workspaceID,
    workspace: workspaceSlug,
  });

  const authTokenURL = coreEndpointURL(coreBaseURL, "/auth/token");
  const coreContextHeaders = buildServerCoreWorkspaceContextHeaders({
    organizationSlug: resolvedOrganizationSlug,
    workspaceSlug,
  });
  let tokenExchange;
  try {
    tokenExchange = await postJSONWithNetworkRetries(
      authTokenURL,
      {
        grant_type: "workspace_human_grant",
        assertion,
      },
      { attempts: 10, delayMs: 500, headers: coreContextHeaders },
    );
  } catch (err) {
    logServerEvent(
      "auth.callback.core_token_unreachable",
      {
        path: eventPath,
        workspace: workspaceSlug,
        auth_token_url: authTokenURL,
        detail: fetchErrorDetail(err),
      },
      { level: "error" },
    );
    return respondWithCallbackError(
      event,
      503,
      "workspace_core_unreachable",
      `Could not reach workspace core for token exchange at ${authTokenURL} (${fetchErrorDetail(err)}). If you just created the workspace, wait until anx-core is listening on that port and try again.`,
      { workspace_name: workspaceName },
    );
  }
  if (!tokenExchange.response.ok) {
    const coreCode = String(
      tokenExchange.payload?.error?.code ?? "workspace_token_exchange_failed",
    );
    const coreMessage = String(
      tokenExchange.payload?.error?.message ??
        "Workspace auth token exchange failed.",
    );
    logServerEvent(
      "auth.callback.core_token_failed",
      {
        path: eventPath,
        workspace: workspaceSlug,
        auth_token_url: authTokenURL,
        status: tokenExchange.response.status || 502,
        code: coreCode,
      },
      { level: "warn" },
    );
    return respondWithCallbackError(
      event,
      tokenExchange.response.status || 502,
      coreCode,
      coreMessage,
      { workspace_name: workspaceName },
    );
  }

  const tokens = tokenExchange.payload?.tokens ?? {};
  const refreshToken = String(tokens.refresh_token ?? "").trim();
  const accessToken = String(tokens.access_token ?? "").trim();
  if (!refreshToken || !accessToken) {
    logServerEvent(
      "auth.callback.core_token_invalid",
      {
        path: eventPath,
        workspace: workspaceSlug,
      },
      { level: "error" },
    );
    return respondWithCallbackError(
      event,
      502,
      "invalid_workspace_response",
      "Workspace token exchange response was missing required tokens.",
      { workspace_name: workspaceName },
    );
  }

  writeWorkspaceRefreshToken(
    event,
    resolvedOrganizationSlug,
    workspaceSlug,
    refreshToken,
  );
  writeWorkspaceAccessToken(
    event,
    resolvedOrganizationSlug,
    workspaceSlug,
    accessToken,
  );
  clearRetryableWorkspaceAuthFailureCount(
    event,
    resolvedOrganizationSlug,
    workspaceSlug,
  );
  logServerEvent("auth.callback.redirect", {
    path: eventPath,
    workspace: workspaceSlug,
    return_path: returnPath,
  });

  throw redirect(
    303,
    workspacePath(resolvedOrganizationSlug, workspaceSlug, returnPath),
  );
}
