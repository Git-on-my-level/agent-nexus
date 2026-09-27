/**
 * Live list updates from the workspace event stream (`GET /stream/events`).
 *
 * A list page (Tasks, Docs, …) subscribes with the event types that can
 * change it and a callback that re-reads the list. The helper owns the
 * connection: it starts from the newest matching event so history is not
 * replayed, resumes with `last_event_id` after a drop, coalesces bursts into
 * one callback, and stops for good on an auth failure (the page's own reads
 * surface that). There is no Reload button to fall back on, so a dropped
 * connection reconnects on its own, backing off while core is unreachable;
 * the resume cursor makes core replay whatever landed during the gap.
 *
 * Usage (Svelte 5):
 *
 *   onMount(() => liveWorkspaceEvents({
 *     client: coreClient,
 *     types: ["card_created", "card_moved"],
 *     onChange: () => reload(),
 *   }));
 *
 * @param {{
 *   client: {
 *     streamEvents?: (options: object) => Promise<void>,
 *     listEvents?: (filters: object) => Promise<{ events?: object[] }>,
 *   },
 *   types?: string[],
 *   threadId?: string,
 *   filter?: (event: object) => boolean,
 *   onChange: (events: object[]) => void | Promise<void>,
 *   debounceMs?: number,
 *   reconnectMs?: number,
 * }} options
 * @returns {() => void} stop
 */
export function liveWorkspaceEvents({
  client,
  types = [],
  threadId = "",
  filter = () => true,
  onChange,
  debounceMs = 400,
  reconnectMs = 3000,
}) {
  if (
    !client ||
    typeof client.streamEvents !== "function" ||
    typeof onChange !== "function"
  ) {
    return () => {};
  }
  let stopped = false;
  let controller = null;
  let reconnectTimer = null;
  let flushTimer = null;
  let pending = [];
  let lastEventId = "";
  let connectedOnce = false;
  let failures = 0;
  const startedAt = Date.now();

  function flush() {
    flushTimer = null;
    if (stopped || !pending.length) return;
    const batch = pending;
    pending = [];
    void Promise.resolve()
      .then(() => onChange(batch))
      .catch(() => {
        // The page owns its own error state; a failed re-read is not ours.
      });
  }

  function schedule(event) {
    pending.push(event);
    if (flushTimer) clearTimeout(flushTimer);
    flushTimer = setTimeout(flush, debounceMs);
  }

  /** Start after the newest matching event so connecting replays nothing. */
  async function seedCursor() {
    if (typeof client.listEvents !== "function") return;
    try {
      const result = await client.listEvents({
        ...(types.length ? { type: types } : {}),
        ...(threadId ? { thread_id: threadId } : {}),
        limit: 1,
      });
      const newest = Array.isArray(result?.events) ? result.events[0] : null;
      if (newest?.id && !lastEventId) lastEventId = String(newest.id);
    } catch {
      // Without a cursor the timestamp guard below still ignores history.
    }
  }

  function isHistory(event) {
    // Guard for a missing cursor: an event older than the subscription is
    // history the page already loaded.
    const ts = Date.parse(event?.ts ?? event?.created_at ?? "");
    return !connectedOnce && Number.isFinite(ts) && ts < startedAt - 5_000;
  }

  async function connect() {
    if (stopped) return;
    if (!connectedOnce && !lastEventId) await seedCursor();
    if (stopped) return;
    controller = new AbortController();
    let delivered = false;
    const openedAt = Date.now();
    try {
      await client.streamEvents({
        threadId: threadId || undefined,
        types,
        lastEventId: lastEventId || undefined,
        signal: controller.signal,
        onEvent: (message) => {
          delivered = true;
          if (message?.id) lastEventId = String(message.id);
          if (message?.event !== "event") return;
          const event = message?.data?.event;
          if (!event || typeof event !== "object") return;
          if (isHistory(event)) return;
          if (!filter(event)) return;
          schedule(event);
        },
      });
    } catch (error) {
      if (stopped || error?.name === "AbortError") return;
      if (error?.status === 401 || error?.status === 403) {
        stopped = true;
        return;
      }
    }
    connectedOnce = true;
    // A stream that stayed open ended normally (a proxy timeout, a deploy);
    // one that failed straight away waits longer each time.
    const healthy = delivered || Date.now() - openedAt > 10_000;
    failures = healthy ? 0 : failures + 1;
    const delay = Math.min(reconnectMs * 2 ** Math.min(failures, 4), 60_000);
    if (!stopped) reconnectTimer = setTimeout(connect, delay);
  }

  void connect();

  return () => {
    stopped = true;
    controller?.abort();
    clearTimeout(reconnectTimer);
    clearTimeout(flushTimer);
    pending = [];
  };
}

/** Event types that change the Tasks list. */
export const TASK_LIST_EVENT_TYPES = [
  "card_created",
  "card_updated",
  "card_moved",
  "card_archived",
  "card_trashed",
  "card_resolved",
  "board_created",
  "board_updated",
];

/** Event types that change the Docs list (comments update the preview). */
export const DOC_LIST_EVENT_TYPES = [
  "document_created",
  "document_revised",
  "document_trashed",
  "document_restored",
  "message_posted",
];
