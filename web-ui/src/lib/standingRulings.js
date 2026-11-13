/**
 * Standing rulings on Decide: sending an ask back as "not a decision",
 * making an answer always hold, and revoking either.
 *
 * Every write goes through the durable outbox (`mutationOutbox.js`), so a
 * reload or a dropped connection never loses the reader's choice, and a
 * failure shows with a retry instead of a raw error.
 */
import { onInboxResponseCommitted } from "$lib/inboxResponseQueue.js";
import { refreshBrief } from "$lib/briefStore.js";
import {
  enqueueLocalMutation,
  mutationConfirmations,
} from "$lib/mutationOutbox.js";
import { randomUUID } from "$lib/replayPolicy.js";
import {
  NOT_A_DECISION,
  RULING_CREATE,
  RULING_REVOKE,
} from "$lib/standingRulingSend.js";

/** The public reason an asker reads when its ask is sent back. */
export const NOT_A_DECISION_REASON =
  "Not a decision: this is information, not a choice. Use anx tell for it.";

export const STANDING_OPERATIONS = [
  NOT_A_DECISION,
  RULING_CREATE,
  RULING_REVOKE,
];

export const notADecisionTarget = (itemId) => `not-a-decision:${itemId}`;
export const rulingTarget = (askRef) => `ruling:${askRef}`;
export const revokeTarget = (id) => `ruling-revoke:${id}`;

/**
 * A single ask or review backed by an ask event can be sent back or ruled
 * on. A batch, an access or host request and a PM proposal cannot: each has
 * its own way to be answered.
 */
export function canRuleOn(decision) {
  return (
    (decision?.kind === "ask" || decision?.kind === "review") &&
    Boolean(decision.eventRef) &&
    !decision.batch &&
    !decision.curation &&
    !decision.pmDecision &&
    !decision.accessRequestId
  );
}

/** Send an ask back as not a decision; `always` also records a ruling. */
export function enqueueNotADecision({ itemId, askRef, always, title, href }) {
  const key = randomUUID();
  return enqueueLocalMutation({
    key,
    target: notADecisionTarget(itemId),
    operation: NOT_A_DECISION,
    href,
    title: "Send back: not a decision",
    preview: title,
    request: {
      ask_ref: askRef,
      always: Boolean(always),
      body: {
        request_key: key,
        reason: NOT_A_DECISION_REASON,
        evidence: [askRef],
      },
    },
  });
}

/** Make an answer that was sent a standing ruling for its question. */
export function enqueueStandingAnswer({ askRef, answer, title, href }) {
  return enqueueLocalMutation({
    key: randomUUID(),
    target: rulingTarget(askRef),
    operation: RULING_CREATE,
    href,
    title: "Always answer this way",
    preview: title ? `${title} → ${answer}` : answer,
    request: { body: { ask_ref: askRef, kind: "answer", answer } },
  });
}

export function enqueueRulingRevoke({ id, question, href }) {
  return enqueueLocalMutation({
    key: randomUUID(),
    target: revokeTarget(id),
    operation: RULING_REVOKE,
    href,
    title: "Revoke standing ruling",
    preview: question,
    request: { id },
  });
}

/*
 * "Always answer this way" waits for the answer itself. An answer sits behind
 * the undo window first; a ruling made then would outlive an answer the
 * reader took back. So the intent is held here per item and turned into a
 * ruling only when the queue reports that answer committed. Each new answer
 * to the same item replaces the intent (or clears it), so an undone answer
 * followed by an unticked one makes no ruling.
 */
const intents = new Map();

export function setStandingAnswerIntent(itemId, intent) {
  if (intent?.askRef && intent.answer) intents.set(itemId, intent);
  else intents.delete(itemId);
}

/**
 * Called on every answer to `itemId`: records the intent when the reader
 * ticked "Always answer this way" on a decision that can be ruled on, and
 * clears any earlier intent otherwise.
 */
export function intend(itemId, wanted, decision, answer) {
  setStandingAnswerIntent(
    itemId,
    wanted && canRuleOn(decision)
      ? { askRef: decision.eventRef, answer, title: decision.title }
      : null,
  );
}

export function standingAnswerIntent(itemId) {
  return intents.get(itemId) ?? null;
}

/**
 * Item ids of asks the reader sent back: waiting to send, sending, or
 * confirmed. Decide drops those rows at once, the way an answered row
 * leaves; a send that needs the reader (`attention`) brings the row back
 * with its retry.
 */
export function sentBackItemIds(outbox, confirmations) {
  const prefix = notADecisionTarget("");
  return [
    ...(outbox ?? [])
      .filter(
        (entry) =>
          entry.operation === NOT_A_DECISION && entry.state !== "attention",
      )
      .map((entry) => entry.target),
    ...Object.keys(confirmations ?? {}),
  ]
    .filter((target) => String(target ?? "").startsWith(prefix))
    .map((target) => target.slice(prefix.length));
}

let stopListening = null;
/** Idempotent. Called by the decision card, so any page that can answer listens. */
export function watchStandingAnswers() {
  if (stopListening) return;
  const stopCommitted = onInboxResponseCommitted((itemId) => {
    const intent = intents.get(itemId);
    if (!intent) return;
    intents.delete(itemId);
    // A failure here is the outbox's to show; the answer itself is sent.
    void enqueueStandingAnswer(intent).catch(() => {});
  });
  // Once core confirms an ask went back, read the brief again so the row is
  // gone because core says so, not only because this tab hid it.
  const seen = new Set();
  const prefix = notADecisionTarget("");
  const stopConfirmed = mutationConfirmations.subscribe((rows) => {
    for (const target of Object.keys(rows ?? {})) {
      if (!target.startsWith(prefix) || seen.has(target)) continue;
      seen.add(target);
      void refreshBrief()?.catch?.(() => {});
    }
  });
  stopListening = () => {
    stopCommitted();
    stopConfirmed();
  };
}

export function resetStandingAnswers() {
  intents.clear();
  stopListening?.();
  stopListening = null;
}
