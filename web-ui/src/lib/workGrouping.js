import { workMoveRequest, workMoveTitle } from "$lib/workMove.js";
import { get } from "svelte/store";
import { localWorkScope } from "$lib/localWorkScope.js";
import { enqueueLocalMutation } from "$lib/mutationOutbox.js";
import { randomUUID } from "$lib/replayPolicy.js";

/**
 * Grouping writes from the board: put a card under an initiative, fold it
 * into one, and undo either. Every write goes through the device outbox with
 * the key it was saved with; core replays group, fold and unfold by
 * request_key (see docs/grouping-work.md).
 *
 * Undoing a group has no route of its own: it removes the one plan step the
 * group added, through the keyed plan write. The write is computed once and
 * frozen in the entry; see `groupingPlan.js` and the outbox sender.
 *
 * Making a card an initiative (and its undo) is the version-fenced
 * `work.patch` core offers in the tray's `next_actions`: `{top_level}` at the
 * version the tray read. It has no request key; the fence is what makes a
 * retry safe (see `sendTopLevel` in `groupingPlan.js`).
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
  promote: "work.top_level",
  unpromote: "work.top_level",
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
      return `Put ${card} back in Ungrouped`;
    default:
      return `Change grouping for ${card}`;
  }
}

/**
 * A complete immutable intent; retries never substitute newer reads.
 * `stepId` binds an undo-group to the one plan step the group added;
 * `version` fences a promote or unpromote to the card as the reader saw it.
 */
export function groupingDraft(
  verb,
  work,
  parent = null,
  { stepId = "", version = null } = {},
) {
  return JSON.parse(
    JSON.stringify({
      verb,
      work: { ref: work.ref, title: work.title || work.ref },
      parent: parent
        ? { ref: parent.ref, title: parent.title || parent.ref }
        : null,
      ...(stepId ? { stepId } : {}),
      ...(Number.isInteger(version) ? { version } : {}),
    }),
  );
}

export function groupingRequest(draft, actor, key) {
  if (draft?.verb === "move") return workMoveRequest(draft);
  const operation = VERB_OPERATION[draft?.verb];
  const ref = String(draft?.work?.ref ?? "").trim();
  const parent = String(draft?.parent?.ref ?? "").trim();
  if (!operation || !ref) throw new Error("Choose a card to group.");
  if (operation === "work.top_level") {
    if (!Number.isInteger(draft.version) || draft.version < 0)
      throw new Error("Reload the board to make this card an initiative.");
    return {
      operation,
      request: {
        ref,
        body: {
          actor_id: actor,
          if_version: draft.version,
          patch: { top_level: draft.verb === "promote" },
        },
      },
    };
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
