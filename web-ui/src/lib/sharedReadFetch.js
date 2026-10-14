/** Coalesce simultaneous finite GETs. Each caller retains its own cancellation
 * and response body; the transport is cancelled only after its last reader leaves.
 * Entries disappear at settlement, so this is never a response cache. */
export function createSharedReadFetch({ scope = () => "" } = {}) {
  const pending = new Map();
  return function readFetch(fetchFn, url, init = {}) {
    const method = String(init.method || "GET").toUpperCase();
    const headers = new Headers(init.headers);
    if (
      method !== "GET" ||
      !/\/(?:inbox(?:\/summary)?|work|events|agents|home\/unread|pm\/(?:actions|decisions))$/.test(
        String(url).split(/[?#]/)[0],
      ) ||
      /(?:^|\/)stream(?:\/|\?|$)/.test(String(url)) ||
      headers.get("accept")?.includes("text/event-stream")
    )
      return fetchFn(url, init);
    if (init.signal?.aborted) return Promise.reject(init.signal.reason);
    const key = JSON.stringify([
      scope(),
      String(url),
      [...headers.entries()].sort(),
      init.credentials || "same-origin",
      init.cache || "default",
    ]);
    let entry = pending.get(key);
    if (!entry) {
      const controller = new AbortController();
      entry = { controller, readers: 0, settled: false, promise: null };
      pending.set(key, entry);
      const current = entry;
      current.promise = Promise.resolve()
        .then(() => fetchFn(url, { ...init, signal: controller.signal }))
        // Fetch resolves at headers. Keep sharing and cancellation active until
        // the finite body is read too, so a stalled body cannot outlive its
        // readers or start another identical request at the next poll tick.
        .then(async (response) => {
          const body = response.body ? await response.arrayBuffer() : null;
          const buffered = new Response(body, {
            status: response.status,
            statusText: response.statusText,
            headers: response.headers,
          });
          Object.defineProperty(buffered, "url", { value: response.url });
          return buffered;
        })
        .finally(() => {
          current.settled = true;
          if (pending.get(key) === current) pending.delete(key);
        });
    }
    const current = entry;
    current.readers++;
    return new Promise((resolve, reject) => {
      let finished = false;
      const release = () => {
        if (finished) return false;
        finished = true;
        init.signal?.removeEventListener("abort", abort);
        if (--current.readers === 0 && !current.settled) {
          if (pending.get(key) === current) pending.delete(key);
          current.controller.abort();
        }
        return true;
      };
      const abort = () => {
        if (release()) reject(init.signal.reason);
      };
      init.signal?.addEventListener("abort", abort, { once: true });
      current.promise.then(
        (response) => {
          if (release()) resolve(response.clone());
        },
        (error) => {
          if (release()) reject(error);
        },
      );
    });
  };
}
