/**
 * The saved operations this build's outbox knows how to send.
 *
 * Tabs of different builds share one device outbox. A tab must never claim
 * work it cannot send: an older tab that met a newer operation used to mark
 * it "needs attention" with "Unknown saved operation", and Retry could not
 * help while that tab kept draining. Now a tab skips any operation missing
 * here and leaves it for a build that knows it, and a build that knows an
 * operation revives rows an older tab gave up on that way.
 *
 * Add an operation here in the same change that teaches `send` it.
 */
import { PINS_OPERATION } from "$lib/dashboardPins.js";
import {
  NOT_A_DECISION,
  RULING_CREATE,
  RULING_REVOKE,
  SKIP,
  SKIP_MANY,
} from "$lib/standingRulingSend.js";

export const KNOWN_OPERATIONS = Object.freeze(
  new Set([
    "inbox.controls",
    "events.lifecycle",
    "docs.lifecycle",
    "notifications.settings.set",
    "topics.patch",
    "cards.move",
    "cards.patch",
    PINS_OPERATION,
    NOT_A_DECISION,
    RULING_CREATE,
    RULING_REVOKE,
    SKIP,
    SKIP_MANY,
    "curation.ungroup",
    "pm.decisions.create",
    "pm.decision.answer",
    "work.move",
    "work.group",
    "work.fold",
    "work.unfold",
    "work.top_level",
    "work.ungroup",
    "work.create",
    "docs.create",
    "docs.patch",
    "docs.revise",
    "artifacts.attachments.create",
    "events.create",
  ]),
);

/** The diagnostic an older build saved when it met an operation it lacked. */
export const UNKNOWN_OPERATION = "Unknown saved operation";

export const isKnownOperation = (operation) =>
  KNOWN_OPERATIONS.has(String(operation ?? ""));

/** A row an older tab gave up on only because it lacked this operation. */
export function revivable(entry) {
  return (
    entry?.state === "attention" &&
    entry.failure?.detail === UNKNOWN_OPERATION &&
    isKnownOperation(entry.operation)
  );
}
