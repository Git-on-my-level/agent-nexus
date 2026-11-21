/**
 * Skip on Decide: clear decisions without deciding them, one or all at once.
 *
 * A skip is not an answer and not a ruling. Each skipped ask is closed with
 * outcome `skipped` and a note telling the requester nothing was decided, so
 * it can carry on with its default or ask again later. Nothing is recorded
 * that would stop the same question coming back.
 *
 * Skipping waits a few seconds behind an Undo, like an answer. Then every
 * skip goes into the durable outbox (`mutationOutbox.js`) as its own entry,
 * persisted before it is sent, so a reload or a dropped connection never
 * loses it and a failure brings that one row back with a retry.
 */
import { get, writable } from "svelte/store";
import { refreshBrief } from "$lib/briefStore.js";
import {
  enqueueLocalMutation,
  mutationConfirmations,
} from "$lib/mutationOutbox.js";
import { randomUUID } from "$lib/replayPolicy.js";
import { localWorkScope } from "$lib/localWorkScope.js";
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
 * The skip waiting behind Undo: `{ ids, decisions, deadline }`, or null.
 * Only the latest skip is undoable; a newer one sends the earlier one first.
 */
export const pendingSkip = writable(null);
let timer = null;
let current = null;
export const skipSaveFailures = writable(0);

/** Skip these decisions after the undo window. Returns how many will go. */
export function skipDecisions(decisions, { href = "" } = {}) {
  const chosen = (decisions ?? []).filter(canSkip);
  if (!chosen.length) return 0;
  if (current) void flushSkip();
  current = {
    ids: chosen.map((decision) => decision.inboxItemId),
    decisions: chosen,
    href,
    // A skip belongs to the workspace and reader it was made in.
    scope: get(localWorkScope),
    deadline: Date.now() + SKIP_UNDO_MS,
  };
  pendingSkip.set({ ...current });
  timer = setTimeout(() => void flushSkip(), SKIP_UNDO_MS);
  return chosen.length;
}

/** Take back the waiting skip. Returns its item ids, or null. */
export function undoSkip() {
  if (!current) return null;
  const { ids } = current;
  clearTimeout(timer);
  timer = null;
  current = null;
  pendingSkip.set(null);
  return ids;
}

/** Save the waiting skip to the outbox now (window closed, page left). */
export async function flushSkip() {
  if (!current) return;
  const { decisions, href, scope } = current;
  clearTimeout(timer);
  timer = null;
  current = null;
  pendingSkip.set(null);
  // The reader switched workspace or signed out inside the window: these
  // ids mean nothing there, so the skip is dropped, as an answer would be.
  if (scope !== get(localWorkScope)) return;
  watchSkipConfirmations();
  const saved = await Promise.allSettled(
    decisions.map((decision) =>
      enqueueLocalMutation({
        key: randomUUID(),
        target: skipTarget(decision.inboxItemId),
        operation: SKIP,
        href,
        title: "Skip without deciding",
        preview: decision.title,
        request: skipRequest(decision),
      }),
    ),
  );
  skipSaveFailures.set(
    saved.filter((result) => result.status === "rejected").length,
  );
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
}
