/**
 * Live updates from the workspace event stream (`GET /stream/events`).
 *
 * Every live surface (Tasks, Docs, a task page, the Inbox, the sidebar Inbox
 * count, the Agents roster) subscribes here with the event types that can
 * change it and a callback that re-reads what it shows. Subscriptions for the
 * same client and the same scope (the whole workspace, or one backing thread)
 * share one connection, so the shell never holds two streams for the same
 * workspace.
 *
 * The connection starts after the newest event core already has, because the
 * stream replays every matching event when `last_event_id` is empty or
 * unknown. It resumes with `last_event_id` after a drop (core replays what
 * landed during the gap), backs off while core is unreachable, and stops for
 * good on an auth failure (the page's own reads surface that). Each
 * subscriber coalesces bursts into one callback.
 *
 * `liveAgentChanges` is the same pattern over `GET /stream/agents`, core's
 * ephemeral roster invalidation (`agents_changed`, sent once on connect and
 * after run, presence, host and bridge changes). It has no replay cursor, so
 * every connect is itself a change: callers refetch `GET /agents` on each
 * notification and keep a slow fallback timer for restarts.
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
  const wanted = new Set(Array.isArray(types) ? types : []);
  return subscribe({
    client,
    key: `events:${String(threadId ?? "").trim()}`,
    accept: (event) => !wanted.size || wanted.has(event?.type),
    filter,
    onChange,
    debounceMs,
    reconnectMs,
  });
}

/**
 * Roster invalidations from `GET /stream/agents`. `onChange` runs on connect
 * (core's first notification), on every change and after a reconnect.
 *
 * @param {{
 *   client: { streamAgentChanges?: (options: object) => Promise<void> },
 *   onChange: (changes: object[]) => void | Promise<void>,
 *   debounceMs?: number,
 *   reconnectMs?: number,
 * }} options
 * @returns {() => void} stop
 */
export function liveAgentChanges({
  client,
  onChange,
  debounceMs = 250,
  reconnectMs = 3000,
}) {
  if (
    !client ||
    typeof client.streamAgentChanges !== "function" ||
    typeof onChange !== "function"
  ) {
    return () => {};
  }
  return subscribe({
    client,
    key: "agents",
    accept: () => true,
    filter: () => true,
    onChange,
    debounceMs,
    reconnectMs,
  });
}

/** Inbox page progress qualifies partial counts and resumes after reconnect. */
export function liveInboxChanges({
  client,
  onChange,
  debounceMs = 250,
  reconnectMs = 3000,
}) {
  if (
    !client ||
    typeof client.streamInboxItems !== "function" ||
    typeof onChange !== "function"
  ) {
    return () => {};
  }
  return subscribe({
    client,
    key: "inbox",
    accept: () => true,
    filter: () => true,
    onChange,
    debounceMs,
    reconnectMs,
  });
}

/** One subscriber on a shared hub, with its own filter and debounce. */
function subscribe({
  client,
  key,
  accept,
  filter,
  onChange,
  debounceMs,
  reconnectMs,
}) {
  let stopped = false;
  let flushTimer = null;
  let pending = [];

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

  const subscriber = (event) => {
    if (stopped || !accept(event)) return;
    try {
      if (!filter(event)) return;
    } catch {
      return;
    }
    pending.push(event);
    if (flushTimer) clearTimeout(flushTimer);
    flushTimer = setTimeout(flush, debounceMs);
  };

  const release = joinHub(client, key, subscriber, { reconnectMs });

  return () => {
    stopped = true;
    clearTimeout(flushTimer);
    pending = [];
    release();
  };
}

/** @type {WeakMap<object, Map<string, ReturnType<typeof createHub>>>} */
const hubsByClient = new WeakMap();

function joinHub(client, key, subscriber, options) {
  let hubs = hubsByClient.get(client);
  if (!hubs) {
    hubs = new Map();
    hubsByClient.set(client, hubs);
  }
  let hub = hubs.get(key);
  if (!hub || hub.closed) {
    hub = createHub(client, key, options);
    hubs.set(key, hub);
  }
  hub.subscribers.add(subscriber);
  hub.start();
  const joined = hub;
  return () => {
    joined.subscribers.delete(subscriber);
    if (joined.subscribers.size) return;
    joined.close();
    if (hubs.get(key) === joined) hubs.delete(key);
  };
}

/**
 * One stream connection shared by every subscriber of a key: `agents` for
 * the roster invalidation stream, `events:<thread id or empty>` for the
 * workspace event stream.
 */
function createHub(client, key, { reconnectMs = 3000 } = {}) {
  const agents = key === "agents";
  const inbox = key === "inbox";
  const threadId = agents || inbox ? "" : key.slice("events:".length);
  const subscribers = new Set();
  let started = false;
  let controller = null;
  let reconnectTimer = null;
  let lastEventId = "";
  let connectedOnce = false;
  let failures = 0;
  const startedAt = Date.now();

  const hub = {
    subscribers,
    closed: false,
    start() {
      if (started || hub.closed) return;
      started = true;
      void connect();
    },
    close() {
      hub.closed = true;
      controller?.abort();
      clearTimeout(reconnectTimer);
    },
  };

  /** Start after the newest event so connecting replays nothing. */
  async function seedCursor() {
    if (typeof client.listEvents !== "function") return;
    try {
      const result = await client.listEvents({
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

  function notify(event) {
    for (const subscriber of [...subscribers]) {
      subscriber(event);
    }
  }

  async function connect() {
    if (hub.closed) return;
    if (!agents && !inbox && !connectedOnce && !lastEventId) await seedCursor();
    if (hub.closed) return;
    controller = new AbortController();
    let delivered = false;
    const openedAt = Date.now();
    try {
      if (agents) {
        await client.streamAgentChanges({
          signal: controller.signal,
          onEvent: (message) => {
            delivered = true;
            if (message?.event !== "agents_changed") return;
            notify({
              type: "agents_changed",
              revision: message?.data?.revision ?? null,
            });
          },
        });
      } else if (inbox) {
        await client.streamInboxItems({
          lastEventId: lastEventId || undefined,
          signal: controller.signal,
          onEvent: (message) => {
            delivered = true;
            if (message?.event === "inbox_page") {
              // Progress IDs resume the keyset walk, including an empty
              // completion cursor. Item IDs cannot resume a paged sweep.
              if (message?.id) lastEventId = String(message.id);
              notify({
                type: "inbox_page",
                partial: message?.data?.partial === true,
                resume_cursor: message?.data?.resume_cursor ?? "",
              });
            } else if (message?.event === "inbox_item") {
              notify({ type: "inbox_item", item: message?.data?.item });
            }
          },
        });
      } else {
        await client.streamEvents({
          threadId: threadId || undefined,
          lastEventId: lastEventId || undefined,
          signal: controller.signal,
          onEvent: (message) => {
            delivered = true;
            if (message?.id) lastEventId = String(message.id);
            if (message?.event !== "event") return;
            const event = message?.data?.event;
            if (!event || typeof event !== "object") return;
            if (isHistory(event)) return;
            notify(event);
          },
        });
      }
    } catch (error) {
      if (hub.closed || error?.name === "AbortError") return;
      if (error?.status === 401 || error?.status === 403) {
        // A refused session stays refused; a later subscriber starts over.
        hub.close();
        return;
      }
    }
    connectedOnce = true;
    // A stream that stayed open ended normally (a proxy timeout, a deploy);
    // one that failed straight away waits longer each time.
    const healthy = delivered || Date.now() - openedAt > 10_000;
    failures = healthy ? 0 : failures + 1;
    const delay = Math.min(reconnectMs * 2 ** Math.min(failures, 4), 60_000);
    if (!hub.closed) reconnectTimer = setTimeout(connect, delay);
  }

  return hub;
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
