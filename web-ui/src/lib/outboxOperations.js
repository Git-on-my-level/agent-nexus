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
import { writable } from "svelte/store";
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

/**
 * Who recorded a failure. A row this page failed itself is not revived by it
 * again (core said no; the reader decides). A row an older tab marked is
 * revived every time that tab marks it, after the backoff that tab set.
 */
const TAB = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
export const stampFailure = (failure) => ({ ...failure, tab: TAB });

/**
 * Retry for a skip never writes "pending" outside the send lock: an older
 * tab that lacks the operation would claim the row and fail it again. The
 * key waits here, in memory, and the next hold of the lock sends it.
 */
const retrying = new Set();
const sentHere = new Set();
export const retryInLock = (entry) =>
  [SKIP, SKIP_MANY].includes(entry?.operation) || revivable(entry);
export const requestRetry = (key) => void retrying.add(key);
export const retryRequested = (entry) => retrying.has(entry?.key);

/** What the reader can do about an older tab that keeps failing sends. */
export const OLDER_TAB_HELP =
  "An older open tab of this app could not send this. Reload or close your other tabs and windows of this app, then retry.";

/** An older open tab failed a row this page had already sent. */
export const olderTabBlocking = writable(false);

/** An attention row the outbox may send: retried here, or revived. */
export const sendableAttention = (entry) =>
  entry?.state === "attention" &&
  isKnownOperation(entry.operation) &&
  (retrying.has(entry.key) || (revivable(entry) && entry.failure?.tab !== TAB));

/** Under the send lock: take an attention row for sending, or leave it. */
export function claimAttention(entry) {
  if (!sendableAttention(entry)) return false;
  const retried = retrying.delete(entry.key);
  if (!retried && sentHere.has(entry.key)) olderTabBlocking.set(true);
  sentHere.add(entry.key);
  return true;
}
