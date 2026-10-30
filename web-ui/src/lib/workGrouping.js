import {
  DEFAULT_PROMOTION_REASON,
  DEFAULT_UNSORTED_REASON,
  workMoveRequest,
  workMoveTitle,
} from "$lib/workMove.js";
import { get } from "svelte/store";
import { localWorkScope } from "$lib/localWorkScope.js";
import { enqueueLocalMutation } from "$lib/mutationOutbox.js";
import { randomUUID } from "$lib/replayPolicy.js";

/**
 * Moves, legacy grouping and folds use persisted actor-scoped request keys.
 * Existing work.top_level entries are translated by the outbox sender; new
 * promotion intents always use work.move. Plan undo remains only for saved
 * legacy group operations.
 */

export const GROUPING_OPERATIONS = [
  "work.move",
  "work.group",
  "work.fold",
  "work.unfold",
  "work.ungroup",
  "work.top_level",
];
export const isGrouping = (entry) =>
  GROUPING_OPERATIONS.includes(entry?.operation);
/** One grouping change per card at a time. */
export const groupingTarget = (ref) => `work:${ref}:grouping`;
/** Where an Edit from the outbox hands a saved grouping back to the board. */
export const GROUPING_DRAFT_TARGET = "board:grouping";

const VERB_OPERATION = {
  group: "work.group",
  fold: "work.fold",
  unfold: "work.unfold",
  ungroup: "work.ungroup",
  promote: "work.move",
  unpromote: "work.move",
};

/** What the reader is told the saved change does. */
export function groupingTitle(draft) {
  if (draft.verb === "move") return workMoveTitle(draft);
  const { verb, work, parent } = draft;
  const card = `“${work?.title || work?.ref}”`;
  const home = `“${parent?.title || parent?.ref}”`;
  switch (verb) {
    case "group":
      return `Group ${card} under ${home}`;
    case "fold":
      return `Fold ${card} into ${home}`;
    case "unfold":
      return `Unfold ${card}`;
    case "ungroup":
      return `Take ${card} out of ${home}`;
    case "promote":
      return `Make ${card} an initiative`;
    case "unpromote":
      return `Put ${card} back in Unsorted`;
    default:
      return `Change grouping for ${card}`;
  }
}

/**
 * A complete immutable intent; retries never substitute newer reads.
 * `stepId` binds an undo-group to the one plan step the group added;
 * Legacy version fields are retained for saved drafts; moves use replay keys.
 */
export function groupingDraft(
  verb,
  work,
  parent = null,
  { stepId = "", version = null, reason } = {},
) {
  return JSON.parse(
    JSON.stringify({
      verb,
      work: { ref: work.ref, title: work.title || work.ref },
      parent: parent
        ? { ref: parent.ref, title: parent.title || parent.ref }
        : null,
      ...(verb === "promote" || verb === "unpromote"
        ? {
            reason:
              reason ??
              (verb === "promote"
                ? DEFAULT_PROMOTION_REASON
                : DEFAULT_UNSORTED_REASON),
          }
        : {}),
      ...(stepId ? { stepId } : {}),
      ...(Number.isInteger(version) ? { version } : {}),
    }),
  );
}

export function groupingRequest(draft, actor, key) {
  if (draft?.verb === "move") return workMoveRequest(draft, actor, key);
  const operation = VERB_OPERATION[draft?.verb];
  const ref = String(draft?.work?.ref ?? "").trim();
  const parent = String(draft?.parent?.ref ?? "").trim();
  if (!operation || !ref) throw new Error("Choose a card to group.");
  if (draft.verb === "promote" || draft.verb === "unpromote") {
    return workMoveRequest(
      {
        ...draft,
        destination: draft.verb === "promote" ? "root" : "unsorted",
        reason:
          draft.reason ??
          (draft.verb === "promote"
            ? DEFAULT_PROMOTION_REASON
            : DEFAULT_UNSORTED_REASON),
      },
      actor,
      key,
    );
  }
  if (draft.verb !== "unfold" && !parent)
    throw new Error("Choose an initiative to group under.");
  const body = { actor_id: actor, request_key: key };
  if (draft.verb === "group") body.under = parent;
  if (draft.verb === "fold") body.into = parent;
  // `write` stays empty until the first send freezes the exact plan write;
  // every retry then replays that write with this key.
  if (draft.verb === "ungroup")
    return {
      operation,
      request: { ref, parent, stepId: draft.stepId || "", body, write: null },
    };
  return { operation, request: { ref, body } };
}

/**
 * Why a grouping change was not saved. Nothing was sent in any of these
 * cases; the copy says which fix applies.
 */
export function enqueueFailureMessage(error) {
  if (error?.localPersistence)
    return "Couldn’t save this on your device, so it was not sent. Try again.";
  const reason = String(error?.message ?? "").trim();
  if (/sign in/i.test(reason))
    return "Sign in to this workspace again, then retry. Nothing was sent.";
  return reason
    ? `${reason.replace(/\.$/, "")}. Nothing was sent.`
    : "Couldn’t save this grouping change. Nothing was sent.";
}

export function enqueueGrouping(draft, options = {}) {
  const key = randomUUID();
  const actor = JSON.parse(get(localWorkScope))[3];
  return enqueueLocalMutation({
    draftTarget: GROUPING_DRAFT_TARGET,
    ...options,
    ...groupingRequest(draft, actor, key),
    key,
    target: groupingTarget(draft.work.ref),
    draft,
    title: groupingTitle(draft),
    preview: groupingTitle(draft),
  });
}
