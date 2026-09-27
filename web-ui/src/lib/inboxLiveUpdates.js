import { coreClient } from "$lib/coreClient";

/**
 * One workspace event stream shared by every live Inbox consumer (the Inbox
 * page and the sidebar count), so the shell never holds two connections for
 * the same workspace.
 *
 * Listeners are told that *something* happened; each decides what to reload
 * and coalesces bursts itself. The stream resumes from the newest event the
 * workspace already has, because `/stream/events` without a resume id
 * replays the whole history first.
 */

const RECONNECT_BASE_MS = 1_500;
const RECONNECT_MAX_MS = 30_000;
const SHORT_STREAM_MS = 5_000;

/** @type {Map<string, { listeners: Set<(event: any) => void>, stop: () => void }>} */
const connections = new Map();

/**
 * @param {string} workspaceKey the workspace the caller is showing
 * @param {(event: object | null) => void} listener
 * @returns {() => void} unsubscribe
 */
export function subscribeInboxLiveUpdates(workspaceKey, listener) {
  const key = String(workspaceKey ?? "").trim() || "default";
  let connection = connections.get(key);
  if (!connection) {
    const listeners = new Set();
    connection = { listeners, stop: connect(listeners) };
    connections.set(key, connection);
  }
  connection.listeners.add(listener);
  return () => {
    const current = connections.get(key);
    if (!current) return;
    current.listeners.delete(listener);
    if (!current.listeners.size) {
      current.stop();
      connections.delete(key);
    }
  };
}

function connect(listeners) {
  let stopped = false;
  let controller = /** @type {AbortController | null} */ (null);
  let timer = /** @type {ReturnType<typeof setTimeout> | null} */ (null);
  let lastEventId = "";
  let failures = 0;

  const run = async () => {
    if (stopped) return;
    if (typeof coreClient.streamWorkspaceEvents !== "function") return;
    if (!lastEventId && typeof coreClient.listEvents === "function") {
      try {
        const newest = await coreClient.listEvents({ limit: 1 });
        lastEventId = String(newest?.events?.[0]?.id ?? "").trim();
      } catch {
        // Without a resume point the stream replays history; listeners
        // coalesce that burst into one refresh.
      }
    }
    if (stopped) return;
    controller = new AbortController();
    const openedAt = Date.now();
    try {
      await coreClient.streamWorkspaceEvents({
        lastEventId,
        signal: controller.signal,
        onEvent: (message) => {
          if (message?.id) lastEventId = String(message.id);
          if (message?.event !== "event") return;
          failures = 0;
          const event = message?.data?.event ?? null;
          for (const listener of [...listeners]) {
            try {
              listener(event);
            } catch {
              // One listener must not starve the others.
            }
          }
        },
      });
    } catch (err) {
      if (stopped || err?.name === "AbortError") return;
      // A refused session stays refused; the page's own load says so.
      if (err?.status === 401 || err?.status === 403) return;
      failures += 1;
    }
    if (stopped) return;
    // A stream that closes at once is not live; back off instead of spinning.
    if (Date.now() - openedAt < SHORT_STREAM_MS) failures += 1;
    const delay = Math.min(
      RECONNECT_MAX_MS,
      RECONNECT_BASE_MS * 2 ** Math.min(failures, 4),
    );
    timer = setTimeout(run, delay);
  };

  void run();

  return () => {
    stopped = true;
    controller?.abort();
    if (timer) clearTimeout(timer);
  };
}
