/**
 * Grouping writes the outbox sender applies: pure plan edits for undoing a
 * group, and the fenced send that makes a card an initiative.
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

/**
 * Send a fenced `top_level` patch. A retry repeats the same version, so a
 * patch that already landed comes back 409 and is never applied twice. Core
 * asks for a read-back after any 409 (including `outcome_unknown`): the card
 * already past that version and in the wanted state means the change is in
 * effect; anything else means the card changed, and nothing is sent again.
 */
export async function sendTopLevel(client, { ref, body }) {
  const wanted = body.patch.top_level === true;
  try {
    const result = await client.patchWork(ref, body);
    if (!result?.work?.ref)
      throw new Error("Initiative receipt is incomplete.");
    return result;
  } catch (error) {
    if (Number(error?.status) !== 409) throw error;
    const current = await client.getWork(ref, { summary: 1 });
    const work = current?.work;
    if (
      work?.ref &&
      Number(work.version) > body.if_version &&
      (work.top_level === true) === wanted
    )
      return { work };
    error.publicMessage = wanted
      ? "This card changed after you pressed Make it an initiative, so nothing was changed. Reload the board and try again."
      : "This card changed after you pressed Undo, so it is still an initiative. Reload the board and try again.";
    throw error;
  }
}
