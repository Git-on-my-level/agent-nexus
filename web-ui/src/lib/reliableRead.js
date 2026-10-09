/** Retry only reads: authentication and mutation failures must never be replayed. */
export function isTransientReadError(error) {
  const status = error?.status ?? error?.coreHttpStatus;
  if (status) return [502, 503, 504].includes(status);
  return (
    error instanceof TypeError ||
    /network|fetch|timed?\s*out|timeout|Unable to reach anx-core/i.test(
      error?.message || "",
    )
  );
}

function pause(ms, signal) {
  return new Promise((resolve, reject) => {
    const abort = () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", abort);
      reject(signal.reason);
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", abort, { once: true });
    if (signal?.aborted) abort();
  });
}

// Long enough for an idle core to wake. The failure window includes requests,
// rather than starting a fresh thirty-second grace period on every attempt.
export async function reliableRead(
  read,
  { signal, onRetry, failureWindowMs = 30_000, attemptMs = 45_000 } = {},
) {
  const started = Date.now();
  let attempt = 0;
  while (true) {
    if (signal?.aborted) throw signal.reason;
    const timeout = new AbortController();
    const timer = setTimeout(
      () => timeout.abort(new Error("Loading timed out")),
      attempt === 0
        ? attemptMs
        : Math.max(
            1,
            Math.min(attemptMs, failureWindowMs - (Date.now() - started)),
          ),
    );
    const combined = signal
      ? AbortSignal.any([signal, timeout.signal])
      : timeout.signal;
    try {
      return await new Promise((resolve, reject) => {
        const abort = () => reject(combined.reason);
        combined.addEventListener("abort", abort, { once: true });
        Promise.resolve()
          .then(() => read(combined))
          .then(resolve, reject)
          .finally(() => combined.removeEventListener("abort", abort));
      });
    } catch (error) {
      if (
        signal?.aborted ||
        !isTransientReadError(error) ||
        Date.now() - started >= failureWindowMs
      )
        throw error;
      onRetry?.(error);
    } finally {
      clearTimeout(timer);
    }
    await pause(Math.min(1_000 * 2 ** attempt++, 8_000), signal);
  }
}
