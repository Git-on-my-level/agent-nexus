/** Bounded retries for transient activation failures, independent of route hydration. */
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
    const response = await fetchFn(url, {
      method: "POST",
      credentials: "same-origin",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(input),
      signal: AbortSignal.timeout(15_000),
    });
    if (!isCurrent()) return null;
    if (response.ok) return response.json();
    if (![429, 503].includes(response.status) || attempt === 3)
      throw new Error("Could not open this workspace. Please try again.");
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
