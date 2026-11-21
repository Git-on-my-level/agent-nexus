/**
 * Skip on Decide: clear decisions without deciding them, one or all at once.
 *
 * A skip is not an answer and not a ruling. Each skipped ask is closed with
 * outcome `skipped` and a note telling the requester nothing was decided, so
 * it can carry on with its default or ask again later. Nothing is recorded
 * that would stop the same question coming back.
 *
 * Skipping is journaled at once, then waits a few seconds behind an Undo,
 * like an answer. Then every skip goes into the durable outbox
 * (`mutationOutbox.js`) as its own entry, so a reload, a crash or a dropped
 * connection never loses it and a failure brings that one row back with a
 * retry.
 */
import { get, writable } from "svelte/store";
import { refreshBrief } from "$lib/briefStore.js";
import {
  enqueueLocalMutation,
  mutationConfirmations,
} from "$lib/mutationOutbox.js";
import { randomUUID } from "$lib/replayPolicy.js";
import { localWorkScope, canUseLocalWorkScope } from "$lib/localWorkScope.js";
import { listLocalWork, saveLocalWork } from "$lib/localWorkStore.js";
import { SKIP } from "$lib/standingRulingSend.js";

export { SKIP };

/** What the requester reads. It says plainly that nothing was decided. */
export const SKIP_REASON =
  "Skipped without a decision. Nothing was ruled. Continue on your stated default if it is reversible, or ask again later if this still needs a decision.";

export const SKIP_UNDO_MS = 5_000;

export const skipTarget = (itemId) => `skip:${itemId}`;

/**
 * Asks, reviews, grouped asks and curation proposals answer through
 * POST /inbox/{id}/respond, so they can be skipped. An access or host
 * request and a PM decision have their own routes and outcomes, and stay.
 */
export function canSkip(decision) {
  if (!decision?.inboxItemId) return false;
  if (!["ask", "review", "pm_proposal"].includes(decision.kind)) return false;
  return !decision.pmDecision && !decision.accessRequestId;
}

function skipRequest(decision) {
  // Core wakes only an agent requester; anyone else reads it from their asks.
  const notify = decision.requestedBy?.kind === "agent" ? "original" : "none";
  return {
    item_id: decision.inboxItemId,
    body: {
      response_text: SKIP_REASON,
      outcome: "skipped",
      notify_mode: notify,
      idempotency_key: randomUUID(),
    },
    quiet_key: randomUUID(),
  };
}

/**
 * The skip waiting behind Undo: `{ batch, scope, ids, deadline }`, or null.
 * Only the latest skip is undoable; a newer one sends the earlier one first.
 *
 * The frozen requests are journaled under `skip-pending` the moment the
 * reader skips, before the window starts, so a crash or a killed tab inside
 * the window cannot lose them: `recoverPendingSkips` sends whatever is past
 * its deadline the next time this reader opens the workspace. Undo deletes
 * the journal entry. Sign-out moves the scope's epoch, which discards it.
 */
export const PENDING_KIND = "skip-pending";
export const pendingSkip = writable(null);
let timer = null;
let current = null;
export const skipSaveFailures = writable(0);

function frozenEntry(decision, href) {
  return {
    key: randomUUID(),
    target: skipTarget(decision.inboxItemId),
    operation: SKIP,
    href,
    title: "Skip without deciding",
    preview: decision.title,
    request: skipRequest(decision),
  };
}

/** Skip these decisions after the undo window. Returns how many will go. */
export function skipDecisions(decisions, { href = "" } = {}) {
  const chosen = (decisions ?? []).filter(canSkip);
  if (!chosen.length) return 0;
  const scope = get(localWorkScope);
  if (!canUseLocalWorkScope(scope)) return 0;
  if (current) void flushSkip();
  const batch = randomUUID();
  const deadline = Date.now() + SKIP_UNDO_MS;
  const entries = chosen.map((decision) => frozenEntry(decision, href));
  let saved;
  try {
    // Writes the synchronous journal now; storage failure stops the skip.
    saved = saveLocalWork(scope, PENDING_KIND, batch, { entries, deadline });
  } catch {
    skipSaveFailures.set(chosen.length);
    return 0;
  }
  void saved.catch(() => {});
  current = {
    batch,
    scope,
    entries,
    ids: chosen.map((decision) => decision.inboxItemId),
    deadline,
  };
  pendingSkip.set({ ids: current.ids, deadline });
  timer = setTimeout(() => void flushSkip(), SKIP_UNDO_MS);
  return chosen.length;
}

function forgetPending(scope, batch, reason) {
  try {
    void saveLocalWork(scope, PENDING_KIND, batch, null, {
      deletionReason: reason,
    }).catch(() => {});
  } catch {
    // Signed out: the epoch already discarded it.
  }
}

/** Take back the waiting skip. Returns its item ids, or null. */
export function undoSkip() {
  if (!current) return null;
  const { ids, scope, batch } = current;
  clearTimeout(timer);
  timer = null;
  current = null;
  pendingSkip.set(null);
  forgetPending(scope, batch, "undo");
  return ids;
}

/**
 * Hand a journaled batch to the outbox, under the keys frozen when the
 * reader skipped. Sending a batch twice (two tabs recovering at once) reuses
 * the same outbox keys and idempotency keys, so core records each skip once.
 */
async function sendBatch(scope, batch, entries) {
  watchSkipConfirmations();
  const saved = await Promise.allSettled(
    entries.map((entry) => enqueueLocalMutation(entry)),
  );
  const failed = saved.filter((result) => result.status === "rejected").length;
  skipSaveFailures.set(failed);
  // Keep the journal until every entry is in the outbox.
  if (!failed) forgetPending(scope, batch, "sent");
}

/** Send the waiting skip now (window closed, page left). */
export async function flushSkip() {
  if (!current) return;
  const { scope, batch, entries } = current;
  clearTimeout(timer);
  timer = null;
  current = null;
  pendingSkip.set(null);
  // The reader moved to another workspace inside the window. The outbox
  // writes to the active one, so the journaled skip waits until this reader
  // is back there; sign-out discards it.
  if (scope !== get(localWorkScope)) return;
  await sendBatch(scope, batch, entries);
}

/**
 * Send journaled skips whose undo window has closed: left behind by a crash,
 * a killed tab, or a workspace switch. One still inside its window (this
 * tab's own, or another tab's) is left alone; the timer sends it.
 */
export async function recoverPendingSkips() {
  const scope = get(localWorkScope);
  if (!canUseLocalWorkScope(scope)) return;
  let records = [];
  try {
    records = await listLocalWork(scope, PENDING_KIND);
  } catch {
    return;
  }
  for (const record of records) {
    const { entries, deadline } = record.value ?? {};
    if (record.target === current?.batch || !Array.isArray(entries)) continue;
    if (Number(deadline) > Date.now()) {
      clearTimeout(recoveryTimer);
      recoveryTimer = setTimeout(
        () => void recoverPendingSkips(),
        deadline - Date.now() + 50,
      );
      continue;
    }
    if (scope !== get(localWorkScope)) return;
    await sendBatch(scope, record.target, entries);
  }
}

let stopRecovery = null;
let recoveryTimer = null;
/** Idempotent: recover on mount and whenever the reader's scope changes. */
export function watchPendingSkips() {
  if (stopRecovery) return;
  stopRecovery = localWorkScope.subscribe(() => {
    void recoverPendingSkips();
  });
}

/**
 * Item ids being skipped: behind Undo, waiting to send, sending, or
 * confirmed. Decide drops those rows at once; a send that needs the reader
 * (`attention`) brings its row back.
 */
export function skippingItemIds(pending, outbox, confirmations) {
  const prefix = skipTarget("");
  return [
    ...(pending?.ids ?? []),
    ...[
      ...(outbox ?? [])
        .filter(
          (entry) => entry.operation === SKIP && entry.state !== "attention",
        )
        .map((entry) => entry.target),
      ...Object.keys(confirmations ?? {}),
    ]
      .filter((target) => String(target ?? "").startsWith(prefix))
      .map((target) => target.slice(prefix.length)),
  ];
}

/** Skips the outbox could not send and that need the reader. */
export function stuckSkips(outbox) {
  return (outbox ?? []).filter(
    (entry) => entry.operation === SKIP && entry.state === "attention",
  );
}

let stopConfirmed = null;
let refreshTimer = null;
/**
 * Once core confirms skips, read the brief again so the rows are gone
 * because core says so. A bulk skip confirms many at once: one read after
 * they settle, not one per row.
 */
export function watchSkipConfirmations() {
  if (stopConfirmed) return;
  const seen = new Set(Object.keys(get(mutationConfirmations) ?? {}));
  const prefix = skipTarget("");
  stopConfirmed = mutationConfirmations.subscribe((rows) => {
    let fresh = false;
    for (const target of Object.keys(rows ?? {})) {
      if (!target.startsWith(prefix) || seen.has(target)) continue;
      seen.add(target);
      fresh = true;
    }
    if (!fresh) return;
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(
      () => void refreshBrief()?.catch?.(() => {}),
      400,
    );
  });
}

export function resetDecideSkip() {
  clearTimeout(timer);
  clearTimeout(refreshTimer);
  timer = null;
  current = null;
  pendingSkip.set(null);
  skipSaveFailures.set(0);
  stopConfirmed?.();
  stopConfirmed = null;
  stopRecovery?.();
  stopRecovery = null;
  clearTimeout(recoveryTimer);
  recoveryTimer = null;
}
