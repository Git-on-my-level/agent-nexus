import { get, writable } from "svelte/store";
import { isInboxResponseOutcome } from "$lib/askDelivery.js";
import { captureInboxResponseSender } from "$lib/coreClient";
import { errorMessage } from "$lib/pm/presentation.js";
import { authenticatedAgent } from "$lib/authSession.js";
import { selectedActorId } from "$lib/actorSession.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "$lib/workspaceContext.js";

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
let scopeGeneration = 0;
let scopeIdentity = "";
const responseScope = () => `${scopeIdentity}:${scopeGeneration}`;
// Failed replies belong to the submitting reader, even when another workspace
// is on screen. Keep their retry payloads private until that identity returns.
const suspendedByIdentity = new Map();

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
 * Where a response is going, as of now: the workspace-bound sender and the
 * scope that owns it.
 *
 * Queueing captures this itself, which is right when the reader answers and
 * the response is queued in the same breath. It is not right when something
 * sits between the two — the Inbox flashes a chosen suggestion for a moment
 * before sending it, and a reader can switch workspace inside that moment.
 * Capturing at the choice and handing the binding to `queueInboxResponse`
 * keeps the answer pointed at the workspace it was written in.
 *
 * @returns {{ send: Function, scope: string, identity: string }}
 */
export function captureInboxResponseBinding() {
  return {
    send: captureInboxResponseSender(),
    scope: responseScope(),
    identity: scopeIdentity,
  };
}

/**
 * Queue one response. A response already waiting is committed at once: only
 * the latest action is ever undoable, as in a mail client.
 *
 * @param {{ itemId: string, item?: object, request: object, message: string, restore?: any, binding?: { send: Function, scope: string, identity: string } }} entry
 *   `request` is the exact `respondInboxItem` body; `message` is the toast
 *   copy ("Sent to Omar Reed"); `restore` is handed back on undo; `binding`
 *   is a `captureInboxResponseBinding()` taken earlier, for a response whose
 *   send is separated from the reader's decision.
 * @returns {number} queue id
 */
export function queueInboxResponse({
  itemId,
  item,
  request,
  message,
  restore,
  binding,
}) {
  const id = String(itemId ?? "").trim();
  if (!id) throw new Error("queueInboxResponse requires itemId");
  if (!isInboxResponseOutcome(request?.outcome)) {
    throw new Error("queueInboxResponse requires a valid outcome");
  }
  if (pending) void commit(pending);
  // Only this item's failure: answering B must not disarm Retry on A.
  forgetFailure(id);
  const bound = binding ?? captureInboxResponseBinding();
  const entry = {
    id: ++sequence,
    itemId: id,
    item: item ? { ...item } : null,
    scope: bound.scope,
    identity: bound.identity,
    send: bound.send,
    request: { ...request, idempotency_key: crypto.randomUUID() },
    message: String(message ?? "Response sent"),
    restore: restore ?? null,
    deadline: Date.now() + UNDO_WINDOW_MS,
    timer: null,
  };
  /*
   * The reader has already left the workspace this answer belongs to. A
   * response does not outlive a workspace switch — that is what the switch
   * itself does to one already waiting — and an undo toast here would sit in
   * a workspace that cannot even see the item. Send it now, with the sender
   * it was written with.
   */
  if (entry.scope !== responseScope()) {
    pending = entry;
    void commit(entry);
    return entry.id;
  }
  entry.timer = setTimeout(() => void commit(entry), UNDO_WINDOW_MS);
  pending = entry;
  setOverlay(id, {
    item: entry.item,
    scope: entry.scope,
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
    item: entry.item,
    scope: entry.scope,
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
    const result = await entry.send(entry.itemId, entry.request);
    if (entry.scope !== responseScope()) return;
    forgetFailure(entry.itemId);
    setOverlay(entry.itemId, {
      item: entry.item,
      scope: entry.scope,
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
    const failure = {
      error: errorMessage(err),
      outcome: String(entry.request?.outcome ?? ""),
      response_text: String(entry.request?.response_text ?? ""),
      at: new Date().toISOString(),
    };
    const failed = {
      id: entry.id,
      itemId: entry.itemId,
      item: entry.item,
      scope: entry.scope,
      identity: entry.identity,
      send: entry.send,
      request: entry.request,
      message: entry.message,
      restore: entry.restore,
    };
    if (entry.identity !== scopeIdentity) {
      const suspended = suspendedByIdentity.get(entry.identity) || {
        failures: new Map(),
        markers: {},
      };
      suspended.failures.set(entry.itemId, failed);
      suspended.markers[entry.itemId] = failure;
      suspendedByIdentity.set(entry.identity, suspended);
      return;
    }
    failed.scope = responseScope();
    setOverlay(entry.itemId, null);
    setFailure(entry.itemId, failure);
    failedByItem.set(entry.itemId, failed);
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
  // Open and completed reads can straddle the commit. Keep the answered
  // subject through that gap, even after core removes the original open row.
  const known = new Set(
    list.flatMap((item) => [item?.id, item?.inbox_item_id]),
  );
  const retained = Object.entries(entries)
    .filter(
      ([id, entry]) =>
        !known.has(id) &&
        entry.item &&
        entry.scope === responseScope() &&
        !failedById[id] &&
        (!entry.until || entry.until > now),
    )
    .map(([, entry]) => entry.item);
  return [...list, ...retained].map((item) => {
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
  suspendedByIdentity.clear();
  failedByItem.clear();
  clearTimeout(toastTimer);
  toastTimer = null;
  committedListeners.clear();
  restoreSlots.clear();
  inboxResponseToast.set(null);
  inboxResponseOverlay.set({});
  inboxResponseFailures.set({});
}

// Invalidate visible snapshots with their workspace/reader. A generation rejects late
// results even across logout and login as the same person. Page listeners are
// still owned by their mounted component and remain subscribed.
function invalidateResponseScope() {
  const agent = get(authenticatedAgent);
  const identity = JSON.stringify([
    get(currentOrganizationSlug),
    get(currentWorkspaceSlug),
    agent?.agent_id || "",
    agent?.actor_id || get(selectedActorId) || "",
  ]);
  if (identity === scopeIdentity) return;
  // Store subscriptions run after routing stores change. The captured sender
  // still targets the submitting workspace and reader, including auth retries.
  const previous = scopeIdentity ? JSON.parse(scopeIdentity) : [];
  const next = JSON.parse(identity);
  const workspaceChanged = previous[0] !== next[0] || previous[1] !== next[1];
  let held = null;
  if (pending) {
    if (workspaceChanged) void commit(pending);
    else {
      held = pending;
      clearTimeout(held.timer);
      pending = null;
    }
  }
  if (held || failedByItem.size) {
    suspendedByIdentity.set(scopeIdentity, {
      pending: held,
      failures: new Map(failedByItem),
      markers: get(inboxResponseFailures),
    });
  }
  scopeIdentity = identity;
  scopeGeneration += 1;
  failedByItem.clear();
  restoreSlots.clear();
  showToast(null);
  inboxResponseOverlay.set({});
  inboxResponseFailures.set({});
  const suspended = suspendedByIdentity.get(identity);
  if (!suspended) return;
  suspendedByIdentity.delete(identity);
  for (const [id, entry] of suspended.failures) {
    failedByItem.set(id, { ...entry, scope: responseScope() });
  }
  inboxResponseFailures.set(suspended.markers);
  if (suspended.pending) {
    pending = { ...suspended.pending, scope: responseScope() };
    const resumed = pending;
    resumed.timer = setTimeout(
      () => void commit(resumed),
      Math.max(0, resumed.deadline - Date.now()),
    );
    setOverlay(resumed.itemId, {
      item: resumed.item,
      scope: resumed.scope,
      status: "pending",
      response_text: String(resumed.request?.response_text ?? ""),
      outcome: String(resumed.request?.outcome ?? ""),
      responded_at: new Date().toISOString(),
    });
    showToast({
      id: resumed.id,
      itemId: resumed.itemId,
      message: resumed.message,
      state: "pending",
      deadline: resumed.deadline,
    });
  }
}
for (const store of [
  authenticatedAgent,
  selectedActorId,
  currentOrganizationSlug,
  currentWorkspaceSlug,
]) {
  store.subscribe(invalidateResponseScope);
}
