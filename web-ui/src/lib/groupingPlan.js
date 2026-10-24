/** Pure plan edits for undoing a group. The outbox sender applies them. */

/**
 * The parent plan without the step that grouped `ref`.
 *
 * `changed: false` means the step is already gone, which is also how an
 * earlier send that landed looks on retry. A step another step now waits on
 * is not removed: dropping it would rewrite someone else's dependency.
 */
export function removeGroupedStep(plan, ref) {
  const steps = Array.isArray(plan?.steps) ? plan.steps : null;
  if (!steps) throw new Error("Plan read is incomplete.");
  const removed = steps.filter((step) => step?.ref === ref);
  if (!removed.length) return { changed: false, plan };
  const ids = new Set(removed.map((step) => step.id));
  const kept = steps.filter((step) => step?.ref !== ref);
  if (
    kept.some((step) =>
      (Array.isArray(step?.after) ? step.after : []).some((id) => ids.has(id)),
    )
  )
    throw Object.assign(new Error("Another step depends on this card."), {
      status: 409,
      publicMessage:
        "Another step in this initiative now depends on this card, so it was left in place. Edit the plan to take it out.",
    });
  return { changed: true, plan: { ...plan, steps: kept } };
}

/**
 * The key for one plan write. Each send reads the current plan, so the body
 * is a function of that plan's token: the same token gives the same body and
 * replays exactly, and a newer token is a new request rather than a reuse of
 * a key for another body.
 */
export const ungroupPlanKey = (key, token) => `${key}:${token}`;
