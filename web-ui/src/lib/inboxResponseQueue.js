import { consumeUploadedAttachments } from "$lib/attachmentWork.js";
import { get, writable } from "svelte/store";
import { isInboxResponseOutcome } from "$lib/askDelivery.js";
import { captureInboxResponseSender } from "$lib/coreClient";
import { mutationFailure } from "$lib/mutationStatus.js";
import { replayExpired, expiredReplayError } from "$lib/replayPolicy.js";
import {
  listLocalWork,
  loadLocalWork,
  loadLocalWorkRecord,
  localWorkPrincipal,
  localWorkEpoch,
  clearPrincipalLocalWork,
  onLocalWorkChanged,
  saveLocalWork,
} from "$lib/localWorkStore.js";
import { canUseLocalWorkScope } from "$lib/localWorkScope.js";
import { authenticatedAgent } from "$lib/authSession.js";
import { selectedActorId } from "$lib/actorSession.js";
import { commitInboxView } from "$lib/inboxViewCache.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
  devActorMode,
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

/** Persisted sends for the current workspace/principal, including attention. */
export const inboxUnsentResponses = writable([]);
const inFlight = new Set();
/** Idempotency keys the reader took back. Recovery must not send these
 * while the persisted tombstone is still in flight. */
const cancelledSends = new Set();
const revokedPrincipals = new Set();
let recoveryTimer;
let recovering = false;
let recoveryRequested = false;

function persistedEntry(entry, state = "pending", failure = null) {
  return {
    itemId: entry.itemId,
    item: entry.item,
    identity: entry.identity,
    request: entry.request,
    message: entry.message,
    restore: entry.restore,
    deadline: entry.deadline,
    createdAt: entry.createdAt,
    attempts: entry.attempts || 0,
    nextAt: entry.nextAt || entry.deadline,
    state,
    failure,
  };
}

function persistResponse(entry, state = "pending", failure = null) {
  return saveLocalWork(
    entry.identity,
    "inbox-send",
    entry.request.idempotency_key,
    persistedEntry(entry, state, failure),
    { epoch: entry.localEpoch },
  );
}

function removePersistedResponse(entry, reason = "confirmed") {
  return saveLocalWork(
    entry.identity,
    "inbox-send",
    entry.request.idempotency_key,
    null,
    { epoch: entry.localEpoch, deletionReason: reason },
  );
}

/** Only a confirmed response leaves the outbox. Undo returns it to a draft. */
async function durableSend(entry) {
  await entry.saved;
  if (navigator.onLine === false) throw new Error("The device is offline.");
  if (!globalThis.navigator?.locks) {
    const error = new Error(
      "Web Locks are unavailable; cross-tab sending is paused.",
    );
    error.status = 400;
    error.publicMessage =
      "Sending is paused in this browser. Your text is saved here. Copy it before sending from another browser.";
    throw error;
  }
  return navigator.locks.request(
    `anx:mutation-send:${entry.identity}`,
    async () => {
      if (entry.localEpoch !== localWorkEpoch(entry.identity)) {
        throw new Error("This identity has signed out.");
      }
      const record = await loadLocalWorkRecord(
        entry.identity,
        "inbox-send",
        entry.request.idempotency_key,
      );
      if (record?.deleted && record.deletionReason === "confirmed") return {};
      const stored = record && !record.deleted ? record.value : null;
      if (record?.deletionReason === "undo" || stored?.state === "superseded") {
        const error = new Error("This response was returned to a draft.");
        error.cancelled = true;
        throw error;
      }
      if (!stored)
        throw new Error(
          "The saved request could not be read. Keep the original response for retry.",
        );
      if (replayExpired(stored.createdAt)) throw expiredReplayError();
      if (
        stored.state === "retrying" &&
        stored.nextAt > Date.now() &&
        !entry.manualRetry
      ) {
        const error = new Error("Another tab scheduled the next retry.");
        error.deferred = true;
        throw error;
      }
      // An old timer cannot bypass another tab's definitive failure/backoff.
      if (stored.state === "attention" && !entry.manualRetry) {
        const error = new Error(
          stored.failure?.detail || "Review this response before retrying.",
        );
        error.status = stored.failure?.status || 400;
        throw error;
      }
      entry.attempts = (stored.attempts || 0) + 1;
      await persistResponse(entry, "sending");
      const result = await entry.send(entry.itemId, entry.request);
      await removePersistedResponse(entry);
      await consumeUploadedAttachments(
        entry.identity,
        entry.request.related_refs,
        entry.localEpoch,
      );
      // Do not clear a newer draft written while this response was sending.
      for (const field of ["reply", "context"]) {
        const target = `inbox:${entry.itemId}:${field}`;
        const draft = await loadLocalWork(entry.identity, "draft", target);
        if (draft === entry.request.response_text) {
          await saveLocalWork(entry.identity, "draft", target, null, {
            epoch: entry.localEpoch,
          });
        }
      }
      return result;
    },
  );
}

function scheduleRecovery(delay = 0) {
  if (typeof window === "undefined") return;
  clearTimeout(recoveryTimer);
  recoveryTimer = setTimeout(() => void recoverInboxResponses(), delay);
}

/** Rebuild failed/pending sends after navigation, reload, reconnect or focus. */
export async function recoverInboxResponses() {
  if (recovering) {
    recoveryRequested = true;
    return;
  }
  if (
    !canUseLocalWorkScope(scopeIdentity) ||
    revokedPrincipals.has(localWorkPrincipal(scopeIdentity))
  )
    return;
  recovering = true;
  const identity = scopeIdentity;
  const scope = responseScope();
  const epoch = localWorkEpoch(identity);
  try {
    const records = await listLocalWork(identity, "inbox-send");
    if (scope !== responseScope() || epoch !== localWorkEpoch(identity)) return;
    const visible = new Map(
      records
        .filter(
          ({ value }) =>
            value.state !== "superseded" &&
            !cancelledSends.has(value.request.idempotency_key),
        )
        .map(({ value }) => [value.request.idempotency_key, value]),
    );
    for (const entry of failedByItem.values()) {
      if (entry.volatile) visible.set(entry.request.idempotency_key, entry);
    }
    inboxUnsentResponses.set([...visible.values()]);
    const liveKeys = new Set(
      records.map(({ value }) => value.request?.idempotency_key),
    );
    for (const [itemId, entry] of failedByItem) {
      if (!entry.volatile && !liveKeys.has(entry.request.idempotency_key))
        forgetFailure(itemId);
    }
    let next = Infinity;
    for (const { value } of records) {
      if (
        identity !== scopeIdentity ||
        scope !== responseScope() ||
        epoch !== localWorkEpoch(identity)
      )
        return;
      if (value.state === "superseded") continue;
      const key = value.request.idempotency_key;
      if (cancelledSends.has(key)) continue;
      const held = failedByItem.get(value.itemId);
      // A failed status write must not expose an older pending record as due,
      // create duplicate rows, or retry in a tight loop while storage is full.
      if (held?.volatile && held.request.idempotency_key === key) continue;
      if (inFlight.has(key) || pending?.request.idempotency_key === key)
        continue;
      const entry = {
        ...value,
        id: ++sequence,
        scope,
        send: captureInboxResponseSender(),
        timer: null,
        saved: Promise.resolve(),
        localEpoch: epoch,
      };
      if (replayExpired(entry.createdAt)) {
        const classified = mutationFailure(expiredReplayError());
        const failure = {
          ...classified,
          error: classified.message,
          response_text: entry.request.response_text,
        };
        entry.failure = failure;
        entry.state = "attention";
        failedByItem.set(entry.itemId, entry);
        setFailure(entry.itemId, failure);
        if (!value.failure?.expired) {
          try {
            await persistResponse(entry, "attention", failure);
          } catch {
            entry.volatile = true;
          }
        }
        continue;
      }
      const failure = value.failure || {
        error: "Waiting to send. Your response is kept on this device.",
        state: "pending",
        response_text: value.request.response_text,
        outcome: value.request.outcome,
      };
      failedByItem.set(entry.itemId, entry);
      setFailure(entry.itemId, failure);
      if (value.state === "attention") continue;
      const due = value.nextAt || value.deadline || 0;
      if (due > Date.now()) {
        next = Math.min(next, due - Date.now());
        continue;
      }
      if (navigator.onLine === false) continue;
      // A row's request never occupies the one Undo slot during recovery.
      await commit(entry, true);
    }
    if (Number.isFinite(next)) scheduleRecovery(next);
  } finally {
    recovering = false;
    if (recoveryRequested) {
      recoveryRequested = false;
      scheduleRecovery();
    }
  }
}

if (typeof window !== "undefined") {
  window.addEventListener("online", () => scheduleRecovery());
  window.addEventListener("focus", () => scheduleRecovery());
  onLocalWorkChanged(() => scheduleRecovery());
  window.addEventListener("anx:local-work-purged", (event) => {
    revokedPrincipals.add(event.detail?.principal);
    for (const identity of suspendedByIdentity.keys()) {
      if (localWorkPrincipal(identity) === event.detail?.principal)
        suspendedByIdentity.delete(identity);
    }
    if (event.detail?.principal !== localWorkPrincipal(scopeIdentity)) return;
    if (pending) clearTimeout(pending.timer);
    pending = null;
    suspendedByIdentity.clear();
    failedByItem.clear();
    inboxResponseFailures.set({});
    inboxResponseOverlay.set({});
    inboxUnsentResponses.set([]);
    showToast(null);
  });
}

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
let scopeUsable = false;
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
    localEpoch: localWorkEpoch(scopeIdentity),
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
  if (Array.isArray(request?.answers)) {
    // A batch item answers every member in one request; core records all of
    // them atomically. Outcomes are validated per answer instead.
    if (!request.answers.length) {
      throw new Error("queueInboxResponse requires a non-empty answers array");
    }
    for (const answer of request.answers) {
      if (!isInboxResponseOutcome(answer?.outcome)) {
        throw new Error(
          "queueInboxResponse requires a valid outcome per answer",
        );
      }
    }
  } else if (!isInboxResponseOutcome(request?.outcome)) {
    throw new Error("queueInboxResponse requires a valid outcome");
  }
  const bound = binding ?? captureInboxResponseBinding();
  if (
    bound.localEpoch !== undefined &&
    bound.localEpoch !== localWorkEpoch(bound.identity)
  )
    return 0;
  if (pending) void commit(pending);
  // Only this item's failure: answering B must not disarm Retry on A.
  forgetFailure(id);
  const entry = {
    id: ++sequence,
    itemId: id,
    item: item ? { ...item } : null,
    scope: bound.scope,
    identity: bound.identity,
    send: bound.send,
    request: JSON.parse(
      JSON.stringify({ ...request, idempotency_key: crypto.randomUUID() }),
    ),
    message: String(message ?? "Response sent"),
    restore: restore ?? null,
    deadline: Date.now() + UNDO_WINDOW_MS,
    createdAt: Date.now(),
    timer: null,
    localEpoch: bound.localEpoch ?? localWorkEpoch(bound.identity),
  };
  // Write synchronously to the fallback journal now, not after Undo closes.
  entry.saved = persistResponse(entry);
  if (entry.identity === scopeIdentity)
    inboxUnsentResponses.update((records) => [
      ...records,
      persistedEntry(entry),
    ]);
  entry.saved = entry.saved.then(async () => {
    // A deliberate new answer replaces attention entries, keeping their text
    // as recovery history. Uncertain sends still retry their original key.
    const previous = await listLocalWork(entry.identity, "inbox-send");
    for (const { value } of previous) {
      if (
        value.itemId === entry.itemId &&
        value.state === "attention" &&
        value.request.idempotency_key !== entry.request.idempotency_key
      ) {
        await saveLocalWork(
          entry.identity,
          "inbox-send",
          value.request.idempotency_key,
          { ...value, state: "superseded" },
        );
      }
    }
  });
  void entry.saved.catch(() => {});
  scheduleRecovery();
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
  cancelledSends.add(entry.request.idempotency_key);
  inboxUnsentResponses.update((records) =>
    records.filter(
      (record) =>
        record.request.idempotency_key !== entry.request.idempotency_key,
    ),
  );
  const text = String(
    entry.restore?.draft ||
      entry.restore?.reply ||
      entry.request.response_text ||
      "",
  );
  // Tombstone the outbox immediately so recovery cannot send a reply the
  // reader already took back, even if restoring the draft then fails.
  void Promise.resolve(entry.saved)
    .catch(() => {})
    .then(() => removePersistedResponse(entry, "undo"))
    .then(() => cancelledSends.delete(entry.request.idempotency_key))
    .catch(() => {});
  if (entry.restore?.drafts && typeof entry.restore.drafts === "object") {
    // A batch answer hands every section's text back to its own durable
    // draft, so reopening the item restores each ask's reply in place.
    for (const [ref, sectionText] of Object.entries(entry.restore.drafts)) {
      void saveLocalWork(
        entry.identity,
        "draft",
        `inbox:${entry.itemId}:reply:${ref}`,
        String(sectionText ?? ""),
      ).catch(() => {});
    }
  } else {
    void saveLocalWork(
      entry.identity,
      "draft",
      `inbox:${entry.itemId}:reply`,
      text,
    ).catch(() => {});
  }
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
export function retryInboxResponse(itemId = "", { asNew = false } = {}) {
  const want = String(itemId ?? "").trim();
  const failed = want ? failedByItem.get(want) : newestFailure();
  if (!failed) return Promise.resolve();
  const expired = replayExpired(failed.createdAt);
  if (expired && !asNew) {
    scheduleRecovery();
    return Promise.resolve();
  }
  if (expired) {
    queueInboxResponse({
      itemId: failed.itemId,
      item: failed.item,
      request: failed.request,
      message: failed.message,
      restore: failed.restore,
    });
    return Promise.resolve();
  }
  const entry = {
    ...failed,
    deadline: Date.now(),
    timer: null,
    manualRetry: true,
  };
  entry.saved = persistResponse(entry);
  forgetFailure(entry.itemId);
  /*
   * A response still inside its undo window is committed first. Only one can
   * be `pending`, and overwriting it would strand it: its timer would fire on
   * an entry the queue no longer recognises and send nothing. This is what
   * `queueInboxResponse` does with a superseded response, for the same reason.
   */
  if (pending) void commit(pending);
  pending = entry;
  showToast({
    id: entry.id,
    itemId: entry.itemId,
    message: entry.message,
    state: "sending",
    deadline: entry.deadline,
  });
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
  const entry = failedByItem.get(target);
  if (entry) {
    // Editing never silently loses the original text or auto-resends it.
    entry.nextAt = 0;
    inboxUnsentResponses.update((records) =>
      records.map((record) =>
        record.request.idempotency_key === entry.request.idempotency_key
          ? { ...record, state: "attention" }
          : record,
      ),
    );
    void persistResponse(entry, "attention", {
      state: "attention",
      error: "Review your saved response before sending.",
      response_text: entry.request.response_text,
    });
    void saveLocalWork(
      entry.identity,
      "draft",
      `inbox:${target}:reply`,
      entry.request.response_text,
    );
    stashInboxRestore(target, {
      ...entry.restore,
      draft: entry.request.response_text,
      reply: entry.request.response_text,
      chosen: "",
    });
  }
  forgetFailure(target);
  // The toast only ever shows one failure; retire it when that is this one.
  inboxResponseToast.update((toast) =>
    toast?.state === "failed" && toast.itemId === target ? null : toast,
  );
}

export function editInboxResponse(itemId) {
  const entry = failedByItem.get(String(itemId));
  if (!entry) return;
  dismissInboxResponseFailure(itemId);
  window.dispatchEvent(
    new CustomEvent("anx:edit-draft", {
      detail: {
        scope: entry.identity,
        target: `inbox:${itemId}:reply`,
        text: entry.request.response_text,
      },
    }),
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

async function commit(entry, recovered = false) {
  if (entry.localEpoch !== localWorkEpoch(entry.identity)) return;
  if (!recovered && pending !== entry) return;
  if (inFlight.has(entry.request.idempotency_key)) return;
  inFlight.add(entry.request.idempotency_key);
  if (pending === entry) pending = null;
  clearTimeout(entry.timer);
  inboxResponseToast.update((toast) =>
    toast?.id === entry.id ? { ...toast, state: "sending" } : toast,
  );
  try {
    const result = await durableSend(entry);
    inboxUnsentResponses.update((records) =>
      records.filter(
        (record) =>
          record.request.idempotency_key !== entry.request.idempotency_key,
      ),
    );
    const respondedAt = new Date().toISOString();
    // Confirmation belongs to the captured reader, even after navigation.
    // The active overlay is cleared on scope changes, so persist directly.
    commitInboxView(entry.identity, {
      answered: {
        ...entry.item,
        id: entry.itemId,
        status: "completed",
        response_text:
          entry.item?.response_text ||
          String(entry.request?.response_text ?? ""),
        outcome: entry.item?.outcome || String(entry.request?.outcome ?? ""),
        responded_at: entry.item?.responded_at || respondedAt,
      },
    });
    if (entry.scope !== responseScope()) return;
    forgetFailure(entry.itemId);
    setOverlay(entry.itemId, {
      item: entry.item,
      scope: entry.scope,
      status: "committed",
      response_text: String(entry.request?.response_text ?? ""),
      outcome: String(entry.request?.outcome ?? ""),
      responded_at: respondedAt,
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
    if (entry.localEpoch !== localWorkEpoch(entry.identity)) return;
    if (err?.deferred) return;
    if (err?.cancelled) {
      setOverlay(entry.itemId, null);
      forgetFailure(entry.itemId);
      return;
    }
    /*
     * Not answered. The overlay goes — an unanswered item must not sit under
     * Handled — and the failure takes its place, so the row comes back
     * carrying the reason and a Retry rather than silently reappearing.
     */
    const classification = mutationFailure(err);
    entry.nextAt =
      Date.now() +
      Math.min(60_000, 2_000 * 2 ** Math.min(entry.attempts || 0, 5));
    const failure = {
      ...classification,
      error: classification.message,
      nextAt: classification.state === "retrying" ? entry.nextAt : null,
      outcome: String(entry.request?.outcome ?? ""),
      response_text: String(entry.request?.response_text ?? ""),
      at: new Date().toISOString(),
    };
    // A persistence failure leaves the original in-memory entry intact.
    let volatile = false;
    try {
      await persistResponse(entry, classification.state, failure);
    } catch {
      volatile = true;
      /* Copy remains available in the item. */
    }
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
      createdAt: entry.createdAt,
      attempts: entry.attempts,
      nextAt: entry.nextAt,
      saved: Promise.resolve(),
      localEpoch: entry.localEpoch,
      volatile,
      state: classification.state,
      failure,
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
    inboxUnsentResponses.update((records) => [
      ...records.filter(
        (record) =>
          record.request.idempotency_key !== entry.request.idempotency_key,
      ),
      persistedEntry(entry, classification.state, failure),
    ]);
    setOverlay(entry.itemId, null);
    setFailure(entry.itemId, failure);
    failedByItem.set(entry.itemId, failed);
    showToast({
      id: entry.id,
      itemId: entry.itemId,
      message: entry.message,
      state: "failed",
      deadline: 0,
      error: failure.error,
      expired: failure.expired,
    });
  } finally {
    inFlight.delete(entry.request.idempotency_key);
    scheduleRecovery();
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
      return {
        ...item,
        response_error: failure.error,
        response_failed: true,
        response_delivery_state: failure.state,
      };
    }
    if (!entry || (entry.until && entry.until <= now)) return item;
    if (entry.status === "pending")
      return {
        ...item,
        response_delivery_state: "pending",
        response_pending: true,
      };
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
  cancelledSends.clear();
  clearTimeout(toastTimer);
  toastTimer = null;
  committedListeners.clear();
  restoreSlots.clear();
  inboxResponseToast.set(null);
  inboxResponseOverlay.set({});
  inboxResponseFailures.set({});
  clearTimeout(recoveryTimer);
  inboxUnsentResponses.set([]);
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
  const usable = canUseLocalWorkScope(identity);
  if (identity === scopeIdentity && usable === scopeUsable) return;
  scopeUsable = usable;
  // Store subscriptions run after routing stores change. The captured sender
  // still targets the submitting workspace and reader, including auth retries.
  const previous = scopeIdentity ? JSON.parse(scopeIdentity) : [];
  const next = JSON.parse(identity);
  const workspaceChanged = previous[0] !== next[0] || previous[1] !== next[1];
  const principalChanged =
    !workspaceChanged &&
    !(previous[2] && !next[2]) &&
    canUseLocalWorkScope(identity) &&
    localWorkPrincipal(scopeIdentity) &&
    localWorkPrincipal(identity) &&
    localWorkPrincipal(scopeIdentity) !== localWorkPrincipal(identity);
  if (usable && localWorkPrincipal(identity))
    revokedPrincipals.delete(localWorkPrincipal(identity));
  if (principalChanged) {
    void clearPrincipalLocalWork(scopeIdentity);
    suspendedByIdentity.delete(scopeIdentity);
  }
  let held = null;
  if (pending) {
    if (workspaceChanged) void commit(pending);
    else {
      if (!principalChanged) held = pending;
      clearTimeout(pending.timer);
      pending = null;
    }
  }
  if (!principalChanged && (held || failedByItem.size)) {
    suspendedByIdentity.set(scopeIdentity, {
      pending: held,
      failures: new Map(failedByItem),
      markers: get(inboxResponseFailures),
    });
  }
  scopeIdentity = identity;
  scopeGeneration += 1;
  inboxUnsentResponses.set([]);
  scheduleRecovery();
  failedByItem.clear();
  restoreSlots.clear();
  showToast(null);
  inboxResponseOverlay.set({});
  inboxResponseFailures.set({});
  if (!usable) return;
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
  devActorMode,
]) {
  store.subscribe(invalidateResponseScope);
}
