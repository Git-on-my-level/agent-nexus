import {
  WorkspaceFailureKind,
  WorkspaceNotFoundError,
  WorkspaceUnavailableError,
  classifyWorkspaceFailure,
  isAbortError,
} from "./workspaceFailure.js";

/** A successful activation names the signed-in agent; anything else is malformed. */
async function readActivation(response) {
  try {
    const body = await response.json();
    const agentId = body?.agent?.agent_id;
    if (typeof agentId === "string" && agentId.trim()) return body;
  } catch {
    // Unparseable success: fall through.
  }
  return null;
}

/**
 * Bounded retries for transient activation failures, independent of route
 * hydration, classified by {@link classifyWorkspaceFailure}: an outage
 * (including a malformed success) that outlasts the retries rejects with
 * {@link WorkspaceUnavailableError}, a 404 with {@link WorkspaceNotFoundError},
 * and a 401 / 403 with a definitive error.
 */
export async function requestWorkspaceActivation({
  url,
  input,
  fetchFn = fetch,
  onRetry = () => {},
  isCurrent = () => true,
  wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
}) {
  for (let attempt = 0; attempt < 4; attempt++) {
    if (!isCurrent()) return null;
    let response;
    try {
      response = await fetchFn(url, {
        method: "POST",
        credentials: "same-origin",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(input),
        signal: AbortSignal.timeout(15_000),
      });
    } catch (error) {
      if (!isCurrent()) return null;
      if (isAbortError(error)) throw error;
      // Network error or timeout: the workspace did not answer.
      if (attempt === 3) throw new WorkspaceUnavailableError(error);
      onRetry("This workspace is temporarily unavailable. Retrying…");
      await wait(Math.min(30_000, 500 * 2 ** attempt));
      continue;
    }
    if (!isCurrent()) return null;
    if (response.ok) {
      const activation = await readActivation(response);
      if (!isCurrent()) return null;
      if (activation) return activation;
    } else {
      const kind = classifyWorkspaceFailure(response);
      if (kind === WorkspaceFailureKind.NOT_FOUND)
        throw new WorkspaceNotFoundError(response.status);
      if (kind === WorkspaceFailureKind.REJECTED)
        throw new Error("Could not open this workspace. Please try again.");
    }
    if (attempt === 3)
      throw new WorkspaceUnavailableError(
        response.ok ? "malformed_response" : response.status,
      );
    const retryAfter = response.headers.get("retry-after");
    const seconds = Number(retryAfter);
    const serverDelay =
      retryAfter === null
        ? 0
        : Number.isFinite(seconds)
          ? seconds * 1000
          : Date.parse(retryAfter) - Date.now();
    const delay = Math.min(
      30_000,
      Math.max(500 * 2 ** attempt, serverDelay || 0),
    );
    onRetry("This workspace is temporarily unavailable. Retrying…");
    await wait(delay);
  }
  return null;
}
