/**
 * The one workspace failure rule, shared by the browser and the server
 * (`classifyWorkspaceSessionFailure` in `$lib/server/authSession.js` is this
 * function). Every caller that turns a failed session, activation or
 * bootstrap request into a user-facing state goes through it:
 *
 * - `rejected` (401 / 403): the server refused the credentials. Only this
 *   signs the viewer out of this workspace.
 * - `not_found` (404): this workspace does not exist (deleted, never
 *   configured, or not visible to this login). A definitive "not found"
 *   state; the session is kept and nothing else is signed out.
 * - `unavailable` (everything else: 5xx, 429, other 4xx, network errors,
 *   timeouts, malformed or shape-invalid successes): an outage. The session
 *   is kept and the viewer sees a calm, retryable "temporarily unreachable"
 *   state.
 */

export const WorkspaceFailureKind = Object.freeze({
  REJECTED: "rejected",
  NOT_FOUND: "not_found",
  UNAVAILABLE: "unavailable",
});

/**
 * @param {unknown} error an Error carrying `status`, a Response, or anything
 * @returns {"rejected"|"not_found"|"unavailable"}
 */
export function classifyWorkspaceFailure(error) {
  const status = Number(/** @type {any} */ (error)?.status);
  if (status === 401 || status === 403) return WorkspaceFailureKind.REJECTED;
  if (status === 404) return WorkspaceFailureKind.NOT_FOUND;
  return WorkspaceFailureKind.UNAVAILABLE;
}

export const WORKSPACE_UNREACHABLE_MESSAGE =
  "This workspace is temporarily unreachable. Try again in a moment.";

export const WORKSPACE_NOT_FOUND_MESSAGE =
  "This workspace doesn't exist, or it was moved or removed.";

/** Stable code for a success reply that is not the expected shape. */
export const MALFORMED_RESPONSE_CODE = "malformed_response";

export class WorkspaceUnavailableError extends Error {
  /** @param {unknown} [cause] */
  constructor(cause) {
    super(WORKSPACE_UNREACHABLE_MESSAGE, { cause });
    this.name = "WorkspaceUnavailableError";
    this.code = "workspace_unavailable";
  }
}

export class WorkspaceNotFoundError extends Error {
  /** @param {unknown} [cause] */
  constructor(cause) {
    super(WORKSPACE_NOT_FOUND_MESSAGE, { cause });
    this.name = "WorkspaceNotFoundError";
    this.code = "workspace_not_found";
  }
}

/** @param {unknown} error */
export function isWorkspaceUnavailableError(error) {
  return (
    error instanceof WorkspaceUnavailableError ||
    /** @type {any} */ (error)?.code === "workspace_unavailable"
  );
}

/** @param {unknown} error */
export function isWorkspaceNotFoundError(error) {
  return (
    error instanceof WorkspaceNotFoundError ||
    /** @type {any} */ (error)?.code === "workspace_not_found"
  );
}

/**
 * The user-facing error for a failure that is not a sign-out, or `null` for
 * an authoritative rejection.
 *
 * @param {unknown} error
 * @returns {WorkspaceUnavailableError|WorkspaceNotFoundError|null}
 */
export function workspaceFailureError(error) {
  if (isWorkspaceUnavailableError(error) || isWorkspaceNotFoundError(error))
    return /** @type {any} */ (error);
  const kind = classifyWorkspaceFailure(error);
  if (kind === WorkspaceFailureKind.NOT_FOUND)
    return new WorkspaceNotFoundError(error);
  if (kind === WorkspaceFailureKind.UNAVAILABLE)
    return new WorkspaceUnavailableError(error);
  return null;
}

/**
 * What the shell shows when opening a workspace fails.
 *
 * @param {unknown} error
 * @returns {{ message: string, notFound: boolean }}
 */
export function describeActivationFailure(error) {
  if (isWorkspaceNotFoundError(error))
    return { message: WORKSPACE_NOT_FOUND_MESSAGE, notFound: true };
  if (isWorkspaceUnavailableError(error))
    return { message: WORKSPACE_UNREACHABLE_MESSAGE, notFound: false };
  return {
    message: "Could not open this workspace. Please try again.",
    notFound: false,
  };
}

/** @param {unknown} error */
export function isAbortError(error) {
  return /** @type {any} */ (error)?.name === "AbortError";
}
