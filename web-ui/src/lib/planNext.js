/**
 * What happens next on a card, read off the plan core computed.
 *
 * Core answers this on every plan read: `next_steps` is the set of steps whose
 * dependencies are all done and which are neither done nor blocked, and
 * `critical_path` says which of them the plan actually waits on. Nothing here
 * decides what is next — it only names the ids core sent and puts the one core
 * itself picked first, so the page, the Overview tile and the Tasks row cannot
 * disagree about the same card.
 *
 * The second half of the module is about the card's hand-written `next_action`.
 * That annotation is typed once and never recomputed, so on a card whose plan
 * has moved it is the page's most confident lie — "Phase 1 inventory…" under a
 * plan that says Phase 1 is done. A note that names a finished step and no open
 * one has been overtaken by the plan, and this is how a surface can tell.
 */

const asText = (value) => String(value ?? "").trim();

/** Words too common to identify a step by. */
const STOP_WORDS = new Set([
  "a",
  "an",
  "and",
  "are",
  "as",
  "at",
  "be",
  "but",
  "by",
  "for",
  "from",
  "in",
  "into",
  "is",
  "it",
  "of",
  "on",
  "or",
  "so",
  "that",
  "the",
  "then",
  "this",
  "to",
  "up",
  "with",
]);

/** Comparable words: case folded, punctuation dropped, stop words removed. */
function words(value) {
  return asText(value)
    .toLocaleLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim()
    .split(" ")
    .filter((word) => word && !STOP_WORDS.has(word));
}

/** Step status by id, as core computed it; `""` where it said nothing. */
function statusById(planState) {
  const out = new Map();
  for (const step of Array.isArray(planState?.steps) ? planState.steps : []) {
    const id = asText(step?.id);
    if (id) out.set(id, asText(step?.status));
  }
  return out;
}

/**
 * The steps core says are next, in the order a reader should read them.
 *
 * Core's own `next` — the one step every other surface shows — leads, because
 * two surfaces naming a different "next step" for one card is the disagreement
 * this module exists to prevent. The critical path follows, since those are the
 * steps the plan's finish date actually waits on, and the rest keep core's
 * order.
 *
 * @param {{steps?: Array<{id?: string, title?: string, ref?: string}>}|null} plan
 * @param {{next_steps?: string[], critical_path?: string[]}|null} planState
 * @param {{preferred?: string}} [options] core's own `summary.next.id`
 * @returns {Array<{id: string, title: string, ref: string}>}
 */
export function planNextSteps(plan, planState, options = {}) {
  const steps = new Map(
    (Array.isArray(plan?.steps) ? plan.steps : [])
      .map((step) => [
        asText(step?.id),
        {
          id: asText(step?.id),
          title: asText(step?.title) || asText(step?.id),
          ref: asText(step?.ref),
        },
      ])
      .filter(([id]) => id),
  );
  const ready = (
    Array.isArray(planState?.next_steps) ? planState.next_steps : []
  )
    .map(asText)
    .filter((id) => steps.has(id));
  if (!ready.length) return [];

  const readySet = new Set(ready);
  const path = (
    Array.isArray(planState?.critical_path) ? planState.critical_path : []
  )
    .map(asText)
    .filter((id) => readySet.has(id));

  const order = [];
  for (const id of [asText(options.preferred), ...path, ...ready]) {
    if (readySet.has(id) && !order.includes(id)) order.push(id);
  }
  return order.map((id) => steps.get(id));
}

/**
 * Has the plan moved past this hand-written next action?
 *
 * True when the note names a step core says is done and names no step that is
 * still open. Both halves matter: "land the inventory PR, then pick an
 * approach" names a finished step *and* a live one, and a reader loses real
 * context if that disappears — while "Phase 1 inventory, assigned to Luna" on a
 * plan whose only mention is a done step is simply out of date.
 *
 * Matched on a step's title words rather than its exact sentence, so a note
 * that quotes a step loosely is still recognised. A one-word title is only
 * matched when the word is long enough to identify something: "ship" in a
 * sentence is not evidence that a step called "Ship" is what it means.
 *
 * @param {string} nextAction
 * @param {{steps?: Array<{id?: string, title?: string}>}|null} plan
 * @param {{steps?: Array<{id?: string, status?: string}>}|null} planState
 */
export function nextActionOvertaken(nextAction, plan, planState) {
  const note = words(nextAction);
  if (!note.length) return false;
  const haystack = ` ${note.join(" ")} `;
  const status = statusById(planState);

  let namesDone = false;
  for (const step of Array.isArray(plan?.steps) ? plan.steps : []) {
    const id = asText(step?.id);
    const title = words(step?.title);
    const identifying =
      title.length > 1 || (title.length === 1 && title[0].length >= 6);
    const named =
      (identifying && haystack.includes(` ${title.join(" ")} `)) ||
      (id.length >= 6 && haystack.includes(` ${words(id).join(" ")} `));
    if (!named) continue;
    if (status.get(id) === "done") namesDone = true;
    else return false;
  }
  return namesDone;
}
