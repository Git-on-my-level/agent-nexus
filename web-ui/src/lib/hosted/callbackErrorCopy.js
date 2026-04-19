/** Verbatim copy from hosted-human SSO plan (Surface 3). */
export const CALLBACK_COPY = {
  exchange_expired: {
    heading: "Session link expired",
    body: "Your workspace session link expired before it was used. This happens if you waited too long or used the browser back button. Return to the dashboard and try again.",
  },
  exchange_invalid: {
    heading: "Session link already used",
    body: "This link has already been used to open the workspace. If you're trying to open the workspace again, return to the dashboard.",
  },
  session_exchange_unreachable: {
    heading: "Control plane unreachable",
    body: "The control plane is temporarily unavailable. Wait a moment and try again.",
  },
  workspace_core_unreachable: {
    heading: "Workspace unreachable",
    body: "The workspace server didn't respond after 10 attempts. Try again in a moment.",
  },
  UNKNOWN: {
    heading: "Couldn't open workspace",
    body: "Something went wrong opening your workspace. Return to the dashboard and try again.",
  },
};

export const CALLBACK_CODES_WITH_TABLE_COPY = new Set([
  "exchange_expired",
  "exchange_invalid",
  "session_exchange_unreachable",
  "workspace_core_unreachable",
]);

/** Hosted callback failures that use the UNKNOWN copy (heading/body) on the error page. */
export const CALLBACK_CODES_UNKNOWN_COPY = new Set([
  "invalid_token",
  "workspace_token_exchange_failed",
  "invalid_workspace_response",
  "invalid_control_plane_response",
  "session_exchange_failed",
]);

/** Canonical BFF codes that use the Surface-3 layout (headings/body from the plan). */
export const CALLBACK_UI_CODES = new Set([
  "exchange_expired",
  "exchange_invalid",
  "session_exchange_unreachable",
  "workspace_core_unreachable",
  "state_mismatch",
]);

/** Optional: rare upstream codes that still use UNKNOWN copy on the error page. */
const OPTIONAL_UNKNOWN_SURFACE_CODES = new Set(["rate_limited"]);

/** All codes that should use Surface-3 (including UNKNOWN-copy failures like `invalid_token`). */
export const CALLBACK_ERROR_SURFACE_CODES = new Set([
  ...CALLBACK_UI_CODES,
  ...CALLBACK_CODES_UNKNOWN_COPY,
  ...OPTIONAL_UNKNOWN_SURFACE_CODES,
]);

export function callbackHeadingForCode(code) {
  if (code === "state_mismatch") {
    return CALLBACK_COPY.UNKNOWN.heading;
  }
  if (CALLBACK_CODES_WITH_TABLE_COPY.has(code)) {
    return /** @type {{ heading: string }} */ (CALLBACK_COPY[code]).heading;
  }
  if (CALLBACK_CODES_UNKNOWN_COPY.has(code)) {
    return CALLBACK_COPY.UNKNOWN.heading;
  }
  if (OPTIONAL_UNKNOWN_SURFACE_CODES.has(code)) {
    return CALLBACK_COPY.UNKNOWN.heading;
  }
  return null;
}

export function callbackBodyForCode(code) {
  if (code === "state_mismatch") {
    return CALLBACK_COPY.UNKNOWN.body;
  }
  if (CALLBACK_CODES_WITH_TABLE_COPY.has(code)) {
    return /** @type {{ body: string }} */ (CALLBACK_COPY[code]).body;
  }
  if (CALLBACK_CODES_UNKNOWN_COPY.has(code)) {
    return CALLBACK_COPY.UNKNOWN.body;
  }
  if (OPTIONAL_UNKNOWN_SURFACE_CODES.has(code)) {
    return CALLBACK_COPY.UNKNOWN.body;
  }
  return null;
}
