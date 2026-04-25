/**
 * Canonical auth / session error codes returned by SvelteKit BFF routes and
 * expected by the client. Prefer importing these constants instead of string
 * literals so new codes are added in one place.
 */
export const AuthErrorCode = Object.freeze({
  SESSION_ENDED_BY_ACCOUNT_STATUS: "session_ended_by_account_status",
  WORKSPACE_RESOLVE_FAILED: "workspace_resolve_failed",
  EXCHANGE_FAILED: "exchange_failed",
  EXCHANGE_EXPIRED: "exchange_expired",
  EXCHANGE_INVALID: "exchange_invalid",
  STATE_MISMATCH: "state_mismatch",
  REFRESH_CONSUMED: "refresh_consumed",
  CP_ACCOUNT_INACTIVE: "cp_account_inactive",
  AUTH_SESSION_RETRYABLE: "auth_session_retryable",
  CORE_UNREACHABLE: "core_unreachable",
  CORE_NOT_CONFIGURED: "core_not_configured",
  AUTH_REQUIRED: "auth_required",
  INVALID_TOKEN: "invalid_token",
  /** SSR detected a runaway navigation / __data.json loop. */
  REQUEST_LOOP_DETECTED: "request_loop_detected",
});

/** @type {Set<string>} */
const KNOWN_CODES = new Set(Object.values(AuthErrorCode));

export function isKnownAuthErrorCode(code) {
  return KNOWN_CODES.has(String(code ?? "").trim());
}

export function allAuthErrorCodes() {
  return Array.from(KNOWN_CODES);
}
