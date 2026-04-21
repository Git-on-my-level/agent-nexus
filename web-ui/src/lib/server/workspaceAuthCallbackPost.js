import { env as privateEnv } from "$env/dynamic/private";
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
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver.js";
import { workspacePath } from "$lib/workspacePaths.js";

function wantsJson(request) {
  return request.headers.get("accept")?.includes("application/json") ?? false;
}

/**
 * Maps CP session-exchange errors to canonical BFF codes (`state_mismatch` vs `exchange_invalid`).
 * @param {number} httpStatus
 * @param {string} cpCode
 * @param {string} cpMessage
 */
function canonicalizeSessionExchangeError(httpStatus, cpCode, cpMessage) {
  const msg = String(cpMessage ?? "");
  if (cpCode === "state_mismatch") {
    return "state_mismatch";
  }
  if (cpCode === "exchange_expired") {
    return "exchange_expired";
  }
  if (cpCode === "exchange_invalid") {
    if (httpStatus === 401 || msg.toLowerCase().includes("state is invalid")) {
      return "state_mismatch";
    }
    return "exchange_invalid";
  }
  return cpCode;
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

/** Node may resolve `localhost` to ::1 while anx-core listens on 127.0.0.1 only. */
function coreBaseUrlForServerFetch(url) {
  const trimmed = String(url ?? "").trim();
  if (!trimmed) {
    return "";
  }
  try {
    const parsed = new URL(trimmed.endsWith("/") ? trimmed : `${trimmed}/`);
    if (parsed.hostname === "localhost") {
      parsed.hostname = "127.0.0.1";
    }
    let out = parsed.toString();
    if (out.endsWith("/")) {
      out = out.slice(0, -1);
    }
    return out;
  } catch {
    return trimmed.replace(/\/+$/, "");
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

async function postJSONWithNetworkRetries(url, body, options) {
  const attempts = Math.max(1, Number(options?.attempts ?? 8));
  const delayMs = Math.max(50, Number(options?.delayMs ?? 400));
  let lastErr = /** @type {unknown} */ (null);
  for (let i = 0; i < attempts; i++) {
    try {
      return await postJSON(url, body);
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
 * cookies (`oar_ui_session_*` / `oar_ui_access_*`). It does **not** clear the
 * control-plane dev session cookie (`oar_cp_dev_access_token`), so switching
 * between hosted workspaces reuses the CP session as intended.
 *
 * @param {import('@sveltejs/kit').RequestEvent} event
 * @param {{ organizationSlug: string, workspaceSlug: string }} routeParams
 * @param {FormData} [formOverride] When set (e.g. root `/auth/callback`), avoids re-reading a consumed request body.
 */
export async function runWorkspaceAuthCallbackPost(event, routeParams, formOverride) {
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: routeParams.organizationSlug,
    workspaceSlug: routeParams.workspaceSlug,
  });
  const workspaceNameEarly = workspaceDisplayName(resolved);

  if (resolved.error || !resolved.workspace) {
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

  const workspaceSlug = resolved.workspaceSlug;
  const coreBaseURL = coreBaseUrlForServerFetch(
    normalizeBaseUrl(resolved.workspace.coreBaseUrl),
  );
  if (!coreBaseURL) {
    return respondWithCallbackError(
      event,
      503,
      "workspace_unavailable",
      "Workspace core endpoint is unavailable.",
      { workspace_name: workspaceNameEarly },
    );
  }

  const controlBaseURL = normalizeBaseUrl(privateEnv.ANX_CONTROL_BASE_URL);
  if (!controlBaseURL) {
    return respondWithCallbackError(
      event,
      503,
      "control_plane_unavailable",
      "Control plane URL is not configured.",
      { workspace_name: workspaceNameEarly },
    );
  }

  const form = formOverride ?? (await event.request.formData());
  const exchangeToken = String(form.get("exchange_token") ?? "").trim();
  const state = String(form.get("state") ?? "").trim();
  const formWorkspaceID = String(form.get("workspace_id") ?? "").trim();
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
    resolved.workspace.workspaceId ?? resolved.workspace.id ?? "",
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

  const sessionExchangeURL = new URL(
    `/workspaces/${encodeURIComponent(workspaceID)}/session-exchange`,
    `${controlBaseURL}/`,
  ).toString();
  let exchanged;
  try {
    exchanged = await postJSON(sessionExchangeURL, {
      exchange_token: exchangeToken,
      state,
    });
  } catch (err) {
    return respondWithCallbackError(
      event,
      503,
      "session_exchange_unreachable",
      `Could not reach control plane for session exchange (${fetchErrorDetail(err)}).`,
      { workspace_name: workspaceName },
    );
  }
  if (!exchanged.response.ok) {
    const cpCode = String(
      exchanged.payload?.error?.code ?? "session_exchange_failed",
    );
    const cpMessage = String(
      exchanged.payload?.error?.message ??
        "Failed to exchange launch session with control plane.",
    );
    const canonical = canonicalizeSessionExchangeError(
      exchanged.response.status || 502,
      cpCode,
      cpMessage,
    );
    return respondWithCallbackError(
      event,
      exchanged.response.status || 502,
      canonical,
      cpMessage,
      { workspace_name: workspaceName },
    );
  }

  const assertion = String(exchanged.payload?.grant?.bearer_token ?? "").trim();
  if (!assertion) {
    return respondWithCallbackError(
      event,
      502,
      "invalid_control_plane_response",
      "Control plane response did not include a workspace grant token.",
      { workspace_name: workspaceName },
    );
  }

  const authTokenURL = new URL("/auth/token", `${coreBaseURL}/`).toString();
  let tokenExchange;
  try {
    tokenExchange = await postJSONWithNetworkRetries(
      authTokenURL,
      {
        grant_type: "workspace_human_grant",
        assertion,
      },
      { attempts: 10, delayMs: 500 },
    );
  } catch (err) {
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
    return respondWithCallbackError(
      event,
      502,
      "invalid_workspace_response",
      "Workspace token exchange response was missing required tokens.",
      { workspace_name: workspaceName },
    );
  }

  writeWorkspaceRefreshToken(event, workspaceSlug, refreshToken);
  writeWorkspaceAccessToken(event, workspaceSlug, accessToken);
  clearRetryableWorkspaceAuthFailureCount(event, workspaceSlug);

  throw redirect(
    303,
    workspacePath(resolved.organizationSlug, workspaceSlug, returnPath),
  );
}
