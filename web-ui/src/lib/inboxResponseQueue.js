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
 *
 * ## A failed write is not a quiet write
 *
 * The optimistic overlay files an answered item under Handled before core has
 * confirmed it. When the commit then failed — a 500 from a quota error is the
 * one that found this — the overlay was simply deleted and the row slid back
 * into Needs you with nothing on it, minutes after the reader had watched it
 * leave. The toast said so, but a toast is gone by the next click.
 *
 * So a failure is now recorded *on the item*: the row stays where it is,
 * carries the error and a Retry, and is not filed as answered. Only a server
 * confirmation does that. `applyResponseOverlay` is what both Inbox surfaces
 * read, so neither can forget.
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
  /** @type {Record<string, { status: string, response_text: string, outcome: string, responded_at: string, until?: number }>} */ ({}),
);

/**
 * Responses whose commit failed, keyed by inbox item id, so the row that
 * failed is the row that says so. Cleared by a retry, a dismissal, or a
 * fresh response to the same item.
 */
export const inboxResponseFailures = writable(
  /** @type {Record<string, { error: string, outcome: string, response_text: string, at: string }>} */ ({}),
);

let sequence = 0;
/** @type {null | { id: number, itemId: string, request: object, message: string, restore: any, timer: any, deadline: number }} */
let pending = null;
/**
 * Responses whose commit failed, by inbox item id, holding what it would take
 * to send each one again.
 *
 * One slot was not enough. The mark on the row is per item, so a reader could
 * see "Not sent" and a Retry on A, answer B, come back, click Retry on A and
 * have nothing happen at all: answering B had cleared the only slot while A's
 * error and its button stayed on screen. A button that does nothing is worse
 * than no button.
 *
 * Insertion order is kept, so the no-argument form (the toast's Retry, which
 * is only ever about the newest failure) resends the most recent one.
 *
 * @type {Map<string, { id: number, itemId: string, request: object, message: string, restore: any }>}
 */
const failedByItem = new Map();
let toastTimer = null;
const committedListeners = new Set();

/** Drop the mark and the resend payload for one item. */
function forgetFailure(itemId) {
  failedByItem.delete(itemId);
  setFailure(itemId, null);
}

/** The newest failure, for callers that do not name an item. */
function newestFailure() {
  let last = null;
  for (const entry of failedByItem.values()) last = entry;
  return last;
}

function setFailure(itemId, value) {
  inboxResponseFailures.update((current) => {
    const next = { ...current };
    if (value) next[itemId] = value;
    else delete next[itemId];
    return next;
  });
}

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
  if (
    !["answered", "approved", "rejected", "acknowledged"].includes(
      request?.outcome,
    )
  ) {
    throw new Error("queueInboxResponse requires a valid outcome");
  }
  if (pending) void commit(pending);
  // Only this item's failure: answering B must not disarm Retry on A.
  forgetFailure(id);
  const entry = {
    id: ++sequence,
    itemId: id,
    request: { ...request, idempotency_key: crypto.randomUUID() },
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
    outcome: String(entry.request?.outcome ?? ""),
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
  forgetFailure(entry.itemId);
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

/**
 * Resend a response whose commit failed. It is not undoable a second time.
 *
 * `itemId` is optional and only guards the call: a Retry on a row must not
 * resend a different item's response, which is possible once the failure lives
 * on the row rather than only in the one toast.
 */
export function retryInboxResponse(itemId = "") {
  const want = String(itemId ?? "").trim();
  const failed = want ? failedByItem.get(want) : newestFailure();
  if (!failed) return Promise.resolve();
  const entry = { ...failed, deadline: Date.now(), timer: null };
  forgetFailure(entry.itemId);
  /*
   * A response still inside its undo window is committed first. Only one can
   * be `pending`, and overwriting it would strand it: its timer would fire on
   * an entry the queue no longer recognises and send nothing. This is what
   * `queueInboxResponse` does with a superseded response, for the same reason.
   */
  if (pending) void commit(pending);
  pending = entry;
  setOverlay(entry.itemId, {
    status: "pending",
    response_text: String(entry.request?.response_text ?? ""),
    outcome: String(entry.request?.outcome ?? ""),
    responded_at: new Date().toISOString(),
  });
  return commit(entry);
}

/** Give up on a failed response: the item stays unanswered, visibly so. */
export function dismissInboxResponseFailure(itemId = "") {
  const want = String(itemId ?? "").trim();
  const target = want || newestFailure()?.itemId || "";
  if (!target) return;
  forgetFailure(target);
  // The toast only ever shows one failure; retire it when that is this one.
  inboxResponseToast.update((toast) =>
    toast?.state === "failed" && toast.itemId === target ? null : toast,
  );
}

export function dismissInboxResponseToast() {
  // The toast goes; the mark on the item stays, because the item is still
  // unanswered and that is the thing the reader has to come back to.
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
    forgetFailure(entry.itemId);
    setOverlay(entry.itemId, {
      status: "committed",
      response_text: String(entry.request?.response_text ?? ""),
      outcome: String(entry.request?.outcome ?? ""),
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
    /*
     * Not answered. The overlay goes — an unanswered item must not sit under
     * Handled — and the failure takes its place, so the row comes back
     * carrying the reason and a Retry rather than silently reappearing.
     */
    setOverlay(entry.itemId, null);
    setFailure(entry.itemId, {
      error: errorMessage(err),
      outcome: String(entry.request?.outcome ?? ""),
      response_text: String(entry.request?.response_text ?? ""),
      at: new Date().toISOString(),
    });
    failedByItem.set(entry.itemId, {
      id: entry.id,
      itemId: entry.itemId,
      request: entry.request,
      message: entry.message,
      restore: entry.restore,
    });
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
export function applyResponseOverlay(
  items,
  overlay,
  now = Date.now(),
  failures = {},
) {
  const list = Array.isArray(items) ? items : [];
  const entries = overlay && typeof overlay === "object" ? overlay : {};
  const failedById = failures && typeof failures === "object" ? failures : {};
  if (!Object.keys(entries).length && !Object.keys(failedById).length) {
    return list;
  }
  return list.map((item) => {
    const id = String(item?.id ?? "");
    const failure = failedById[id];
    const entry = entries[id];
    /*
     * A failure outranks the overlay. An item whose send failed is not
     * answered, however optimistically it was filed a moment ago, and it has
     * to stay where the reader left it with the reason attached.
     */
    if (failure) {
      return { ...item, response_error: failure.error, response_failed: true };
    }
    if (!entry || (entry.until && entry.until <= now)) return item;
    if (String(item?.status ?? "") === "completed") return item;
    return {
      ...item,
      status: "completed",
      responded_at: item?.responded_at || entry.responded_at,
      response_text: item?.response_text || entry.response_text,
      outcome: item?.outcome || entry.outcome,
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
  failedByItem.clear();
  clearTimeout(toastTimer);
  toastTimer = null;
  committedListeners.clear();
  restoreSlots.clear();
  inboxResponseToast.set(null);
  inboxResponseOverlay.set({});
  inboxResponseFailures.set({});
}
