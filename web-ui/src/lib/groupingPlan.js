import {
  DEFAULT_PROMOTION_REASON,
  DEFAULT_UNSORTED_REASON,
} from "$lib/workMove.js";

/**
 * Grouping writes the outbox sender applies: pure plan edits for undoing a
 * group, and translation of queued legacy initiative intents into moves.
 */

/**
 * The parent plan without the step that grouped `ref`, or `changed: false`
 * when there is nothing of this group's left to remove.
 *
 * With `stepId` (the step the group added, from the rollup) only that step is
 * removed, and only while it still points at `ref`. Without it, exactly one
 * step must point at `ref`; more than one is ambiguous and refused.
 *
 * A step another step now waits on is not removed: dropping it would rewrite
 * someone else's dependency. The caller computes this once per undo and
 * freezes the result; it is never recomputed from a later read.
 */
export function removeGroupedStep(plan, ref, stepId = "") {
  const steps = Array.isArray(plan?.steps) ? plan.steps : null;
  if (!steps) throw new Error("Plan read is incomplete.");
  const matches = steps.filter(
    (step) => step?.ref === ref && (!stepId || step.id === stepId),
  );
  if (!matches.length) return { changed: false, plan };
  if (matches.length > 1)
    throw Object.assign(new Error("More than one step points at this card."), {
      status: 409,
      publicMessage:
        "This initiative lists the card more than once, so it was left in place. Edit the plan to take it out.",
    });
  const [step] = matches;
  const kept = steps.filter((entry) => entry !== step);
  if (
    kept.some((entry) =>
      (Array.isArray(entry?.after) ? entry.after : []).includes(step.id),
    )
  )
    throw Object.assign(new Error("Another step depends on this card."), {
      status: 409,
      publicMessage:
        "Another step in this initiative now depends on this card, so it was left in place. Edit the plan to take it out.",
    });
  return { changed: true, stepId: step.id, plan: { ...plan, steps: kept } };
}

/** Deliver saved work.top_level entries without ever sending a top_level patch.
 * The persisted outbox identity supplies the replay key; never allocate a new
 * key during retry, and never infer success from the card's current position.
 */
export async function sendTopLevel(client, { ref, body }, key) {
  if (
    !ref ||
    typeof body?.patch?.top_level !== "boolean" ||
    Object.keys(body.patch).length !== 1 ||
    !(body.request_key || key)
  )
    throw new Error("Saved initiative move is incomplete.");
  const root = body.patch.top_level;
  const move = {
    ...(body.actor_id !== undefined ? { actor_id: body.actor_id } : {}),
    request_key: body.request_key || key,
    ...(root ? { root: true } : { unsorted: true }),
    reason:
      body.reason ??
      (root ? DEFAULT_PROMOTION_REASON : DEFAULT_UNSORTED_REASON),
  };
  const result = await client.moveWork(ref, move);
  if (!result?.work?.ref) throw new Error("Initiative receipt is incomplete.");
  return result;
}
