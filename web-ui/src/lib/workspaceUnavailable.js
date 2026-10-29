/**
 * Client side of the workspace session failure rule (server:
 * `classifyWorkspaceSessionFailure` in `$lib/server/authSession.js`). Only an
 * authoritative rejection (401 / 403) means the viewer is signed out. Any
 * other failure (5xx, 429, network error, timeout) means the workspace is
 * temporarily unreachable: the shell keeps the session and shows a calm,
 * retryable state instead of the sign-in page.
 */

export const WORKSPACE_UNREACHABLE_MESSAGE =
  "This workspace is temporarily unreachable. Try again in a moment.";

export class WorkspaceUnavailableError extends Error {
  /** @param {unknown} [cause] */
  constructor(cause) {
    super(WORKSPACE_UNREACHABLE_MESSAGE, { cause });
    this.name = "WorkspaceUnavailableError";
    this.code = "workspace_unavailable";
  }
}

/** @param {unknown} error */
export function isWorkspaceUnavailableError(error) {
  return (
    error instanceof WorkspaceUnavailableError ||
    /** @type {any} */ (error)?.code === "workspace_unavailable"
  );
}

/**
 * True when a failed request says nothing about the session: no HTTP status
 * (network error, timeout) or a 429 / 5xx answer.
 *
 * @param {unknown} error an Error carrying `status`, or a Response
 */
export function isTransientWorkspaceFailure(error) {
  if (!error || typeof error !== "object") return true;
  const status = Number(/** @type {any} */ (error).status);
  return (
    !Number.isFinite(status) || status === 0 || status === 429 || status >= 500
  );
}

/** @param {unknown} error */
export function isAbortError(error) {
  return /** @type {any} */ (error)?.name === "AbortError";
}
