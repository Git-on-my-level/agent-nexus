import { writable } from "svelte/store";
import { coreClient } from "$lib/coreClient";
import { errorMessage } from "$lib/pm/presentation.js";

/**
 * Inbox responses wait a few seconds before they are committed, so a
 * mis-keyed "2" or a hasty Acknowledge can be taken back. The delay is
 * client-side only: what is committed is exactly the `inbox.respond` call the
 * caller built, sent once the window closes.
 *
 * State lives in this module, not in a component, so a response queued on
 * the standalone item page survives the navigation back to the Inbox and is
 * committed (and undoable) there.
 */

export const UNDO_WINDOW_MS = 5_000;
/** How long a committed response keeps hiding its row while the inbox projection catches up. */
const COMMITTED_OVERLAY_MS = 60_000;
const SENT_TOAST_MS = 2_500;

/**
 * The toast the Inbox renders: one at a time.
 * `state` is "pending" (undoable), "sending", "sent" or "failed".
 */
export const inboxResponseToast = writable(
  /** @type {null | { id: number, itemId: string, message: string, state: string, deadline: number, error?: string }} */ (
    null
  ),
);

/**
 * Items answered locally, keyed by inbox item id, so lists can file them
 * under Handled before core's projection says so.
 */
export const inboxResponseOverlay = writable(
  /** @type {Record<string, { status: string, response_text: string, responded_at: string, until?: number }>} */ ({}),
);

let sequence = 0;
/** @type {null | { id: number, itemId: string, request: object, message: string, restore: any, timer: any, deadline: number }} */
let pending = null;
/** @type {null | { id: number, itemId: string, request: object, message: string, restore: any }} */
let failed = null;
let toastTimer = null;
const committedListeners = new Set();

function setOverlay(itemId, value) {
  inboxResponseOverlay.update((current) => {
    const now = Date.now();
    const next = {};
    for (const [key, entry] of Object.entries(current)) {
      if (!entry?.until || entry.until > now) next[key] = entry;
    }
    if (value) next[itemId] = value;
    else delete next[itemId];
    return next;
  });
}

function showToast(toast) {
  clearTimeout(toastTimer);
  toastTimer = null;
  inboxResponseToast.set(toast);
}

/**
 * Queue one response. A response already waiting is committed at once: only
 * the latest action is ever undoable, as in a mail client.
 *
 * @param {{ itemId: string, request: object, message: string, restore?: any }} entry
 *   `request` is the exact `respondInboxItem` body; `message` is the toast
 *   copy ("Sent to Omar Reed"); `restore` is handed back on undo.
 * @returns {number} queue id
 */
export function queueInboxResponse({ itemId, request, message, restore }) {
  const id = String(itemId ?? "").trim();
  if (!id) throw new Error("queueInboxResponse requires itemId");
  if (pending) void commit(pending);
  failed = null;
  const entry = {
    id: ++sequence,
    itemId: id,
    request: { ...request },
    message: String(message ?? "Response sent"),
    restore: restore ?? null,
    deadline: Date.now() + UNDO_WINDOW_MS,
    timer: null,
  };
  entry.timer = setTimeout(() => void commit(entry), UNDO_WINDOW_MS);
  pending = entry;
  setOverlay(id, {
    status: "pending",
    response_text: String(entry.request?.response_text ?? ""),
    responded_at: new Date().toISOString(),
  });
  showToast({
    id: entry.id,
    itemId: id,
    message: entry.message,
    state: "pending",
    deadline: entry.deadline,
  });
  return entry.id;
}

/**
 * Take back the waiting response. Returns what was queued (so the caller can
 * restore the draft and the selection), or null when nothing is undoable.
 */
export function undoInboxResponse() {
  if (!pending) return null;
  const entry = pending;
  pending = null;
  clearTimeout(entry.timer);
  setOverlay(entry.itemId, null);
  showToast(null);
  return {
    itemId: entry.itemId,
    request: entry.request,
    restore: entry.restore,
  };
}

export function hasPendingInboxResponse() {
  return Boolean(pending);
}

/** Commit the waiting response now (page unload, a newer response). */
export function flushInboxResponse() {
  return pending ? commit(pending) : Promise.resolve();
}

/** Resend a response whose commit failed. It is not undoable a second time. */
export function retryInboxResponse() {
  if (!failed) return Promise.resolve();
  const entry = { ...failed, deadline: Date.now(), timer: null };
  failed = null;
  pending = entry;
  setOverlay(entry.itemId, {
    status: "pending",
    response_text: String(entry.request?.response_text ?? ""),
    responded_at: new Date().toISOString(),
  });
  return commit(entry);
}

export function dismissInboxResponseToast() {
  failed = null;
  showToast(null);
}

/** Called with `(itemId, result)` after a response is committed. */
export function onInboxResponseCommitted(listener) {
  committedListeners.add(listener);
  return () => committedListeners.delete(listener);
}

async function commit(entry) {
  if (pending !== entry) return;
  pending = null;
  clearTimeout(entry.timer);
  inboxResponseToast.update((toast) =>
    toast?.id === entry.id ? { ...toast, state: "sending" } : toast,
  );
  try {
    const result = await coreClient.respondInboxItem(
      entry.itemId,
      entry.request,
    );
    setOverlay(entry.itemId, {
      status: "committed",
      response_text: String(entry.request?.response_text ?? ""),
      responded_at: new Date().toISOString(),
      until: Date.now() + COMMITTED_OVERLAY_MS,
    });
    let showing = false;
    inboxResponseToast.update((toast) => {
      showing = toast?.id === entry.id;
      return showing ? { ...toast, state: "sent" } : toast;
    });
    if (showing) {
      clearTimeout(toastTimer);
      toastTimer = setTimeout(() => {
        inboxResponseToast.update((toast) =>
          toast?.id === entry.id && toast.state === "sent" ? null : toast,
        );
      }, SENT_TOAST_MS);
    }
    for (const listener of [...committedListeners]) {
      try {
        listener(entry.itemId, result);
      } catch {
        // A listener's failure is not the response's failure.
      }
    }
  } catch (err) {
    setOverlay(entry.itemId, null);
    failed = {
      id: entry.id,
      itemId: entry.itemId,
      request: entry.request,
      message: entry.message,
      restore: entry.restore,
    };
    showToast({
      id: entry.id,
      itemId: entry.itemId,
      message: entry.message,
      state: "failed",
      deadline: 0,
      error: errorMessage(err),
    });
  }
}

/**
 * Items as the reader should see them: anything answered here is completed,
 * whether or not core's projection has caught up yet.
 */
export function applyResponseOverlay(items, overlay, now = Date.now()) {
  const list = Array.isArray(items) ? items : [];
  const entries = overlay && typeof overlay === "object" ? overlay : {};
  if (!Object.keys(entries).length) return list;
  return list.map((item) => {
    const entry = entries[String(item?.id ?? "")];
    if (!entry || (entry.until && entry.until <= now)) return item;
    if (String(item?.status ?? "") === "completed") return item;
    return {
      ...item,
      status: "completed",
      responded_at: item?.responded_at || entry.responded_at,
      response_text: item?.response_text || entry.response_text,
    };
  });
}

/**
 * The notify mode both Inbox surfaces use by default: the requester when core
 * can reach them, otherwise nobody (core refuses an unreachable default).
 */
export function defaultNotifyMode(item) {
  return item?.notification_target_status?.resolvable === false
    ? "none"
    : "original";
}

const restoreSlots = new Map();

/**
 * Hands an undone response's composer state to the page that owns it (the
 * standalone item page restores its draft, notify target and attachments).
 */
export function stashInboxRestore(itemId, state) {
  restoreSlots.set(String(itemId ?? ""), state);
}

export function takeInboxRestore(itemId) {
  const key = String(itemId ?? "");
  const state = restoreSlots.get(key) ?? null;
  restoreSlots.delete(key);
  return state;
}

/** Test hook: forget everything queued. */
export function resetInboxResponseQueue() {
  if (pending) clearTimeout(pending.timer);
  pending = null;
  failed = null;
  clearTimeout(toastTimer);
  toastTimer = null;
  committedListeners.clear();
  restoreSlots.clear();
  inboxResponseToast.set(null);
  inboxResponseOverlay.set({});
}
