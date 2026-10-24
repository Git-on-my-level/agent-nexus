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
 * group added, through the keyed plan write. See `groupingPlan.js`.
 */

export const GROUPING_OPERATIONS = [
  "work.group",
  "work.fold",
  "work.unfold",
  "work.ungroup",
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
};

/** What the reader is told the saved change does. */
export function groupingTitle({ verb, work, parent }) {
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
    default:
      return `Change grouping for ${card}`;
  }
}

/** A complete immutable intent; retries never substitute newer reads. */
export function groupingDraft(verb, work, parent = null) {
  return JSON.parse(
    JSON.stringify({
      verb,
      work: { ref: work.ref, title: work.title || work.ref },
      parent: parent
        ? { ref: parent.ref, title: parent.title || parent.ref }
        : null,
    }),
  );
}

export function groupingRequest(draft, actor, key) {
  const operation = VERB_OPERATION[draft?.verb];
  const ref = String(draft?.work?.ref ?? "").trim();
  const parent = String(draft?.parent?.ref ?? "").trim();
  if (!operation || !ref) throw new Error("Choose a card to group.");
  if (draft.verb !== "unfold" && !parent)
    throw new Error("Choose an initiative to group under.");
  const body = { actor_id: actor, request_key: key };
  if (draft.verb === "group") body.under = parent;
  if (draft.verb === "fold") body.into = parent;
  if (draft.verb === "ungroup")
    return { operation, request: { ref, parent, body } };
  return { operation, request: { ref, body } };
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
