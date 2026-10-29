import {
  WorkspaceUnavailableError,
  isAbortError,
  isTransientWorkspaceFailure,
} from "./workspaceUnavailable.js";

/**
 * Bounded retries for transient activation failures, independent of route
 * hydration. A transient failure that outlasts the retries rejects with
 * {@link WorkspaceUnavailableError}; any other failure is definitive.
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
    if (response.ok) return response.json();
    if (!isTransientWorkspaceFailure(response))
      throw new Error("Could not open this workspace. Please try again.");
    if (attempt === 3) throw new WorkspaceUnavailableError(response.status);
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
