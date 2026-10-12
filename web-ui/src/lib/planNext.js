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

/**
 * Words too common to identify a step by.
 *
 * Two sets, because a title can fail to identify a step in two different
 * ways. The first is grammar: "the", "to", "with" are in every sentence. The
 * second is status: a step titled "In review" is named after the state it is
 * in, and those are exactly the words a note about state uses too — "leave
 * this in review until Friday" is about a different thing entirely, and
 * matching it on "in review" hides a note that is still true.
 */
const COMMON_WORDS = new Set([
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

const STATE_WORDS = new Set([
  "active",
  "blocked",
  "closed",
  "complete",
  "completed",
  "done",
  "finished",
  "next",
  "open",
  "pending",
  "progress",
  "ready",
  "review",
  "reviewed",
  "started",
  "todo",
  "waiting",
  "wip",
]);

/**
 * Comparable words, in order: case folded, punctuation dropped.
 *
 * Nothing is removed. Stripping the common words first is what let a title of
 * "In review" collapse to the single word `review` and match any sentence
 * containing it; a phrase is only evidence if it is matched as the author
 * wrote it.
 */
function words(value) {
  return asText(value)
    .toLocaleLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim()
    .split(" ")
    .filter(Boolean);
}

/**
 * Do these words pick out a step, or could they be any sentence about state?
 *
 * Two words at least, and at least one of them has to carry meaning of its
 * own. "Phase 1 inventory" names something; "In review" names a column.
 *
 * Used for a title and for a handle a note spells out in words, because the
 * hazard is the same either way: a step whose id is `in-review` is no more
 * identifiable written as "in review" than its title is.
 */
function identifies(parts) {
  return (
    parts.length >= 2 &&
    parts.some((word) => !COMMON_WORDS.has(word) && !STATE_WORDS.has(word))
  );
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
 * A step is named two ways, and never by one ordinary word. Either the note
 * quotes its title as a phrase — two words or more, in the author's order,
 * carrying at least one word that is neither grammar nor a status — or it
 * names the step's id or ref, which are handles nobody types by accident.
 *
 * A handle counts written either way: `phase-1-inventory` as the author
 * spelled it, or "phase 1 inventory" as a sentence would. Both are several
 * tokens in a row, which is the part that matters — a one-word slug like
 * `approach` is an ordinary verb, and a note that merely uses that word is
 * not naming the step.
 *
 * @param {string} nextAction
 * @param {{steps?: Array<{id?: string, title?: string, ref?: string}>}|null} plan
 * @param {{steps?: Array<{id?: string, status?: string}>}|null} planState
 */
export function nextActionOvertaken(nextAction, plan, planState) {
  const note = words(nextAction);
  if (!note.length) return false;
  const phrases = ` ${note.join(" ")} `;
  // Handles keep their punctuation: `card:release-b` is one token to a
  // reader, and splitting it into words would match "release" in prose.
  const literal = asText(nextAction).toLocaleLowerCase();
  const tokens = handleTokens(nextAction);
  const status = statusById(planState);

  let namesDone = false;
  for (const step of Array.isArray(plan?.steps) ? plan.steps : []) {
    const id = asText(step?.id);
    const title = words(step?.title);
    const named =
      (identifies(title) && phrases.includes(` ${title.join(" ")} `)) ||
      namesHandle(literal, tokens, id) ||
      namesHandle(literal, tokens, asText(step?.ref));
    if (!named) continue;
    if (status.get(id) === "done") namesDone = true;
    else return false;
  }
  return namesDone;
}

/**
 * Is this exact handle in the note?
 *
 * A handle is an address — `phase-1-inventory`, `card:release-b`, a URL — and
 * one of those in a sentence was put there on purpose, so one is enough where
 * one word of a title is not. It has to carry a separator to qualify: a step
 * whose id is the bare word `ship` is indistinguishable from the verb, and
 * "ship the docs rewrite" is a note about shipping something else.
 *
 * Bounded on both sides by anything a handle could continue with, not just by
 * letters: `phase-1-inventory-v2` is a different step from
 * `phase-1-inventory`, and so is the pull request one path segment deeper. A
 * full stop is a sentence ending rather than a continuation, so a handle at
 * the end of a sentence still counts.
 */
const HANDLE_CHAR = /[\p{L}\p{N}_:/-]/u;

/**
 * The note as a reader's tokens, with a hyphenated handle left whole.
 *
 * `see phase-1-inventory-v2 instead` is three tokens, and the middle one is
 * not the handle `phase-1-inventory` — which is the whole point of keeping
 * the hyphens that `words` drops.
 */
function handleTokens(value) {
  return asText(value)
    .toLocaleLowerCase()
    .replace(/[^\p{L}\p{N}-]+/gu, " ")
    .trim()
    .split(" ")
    .filter(Boolean);
}

function namesHandle(literal, tokens, handle) {
  const text = handle.toLocaleLowerCase();
  if (text.length < 4) return false;
  const parts = text.split(/[-_.:/]+/).filter(Boolean);
  if (parts.length < 2) return false;

  // As written: bounded, so `phase-1-inventory` is not found inside
  // `phase-1-inventory-v2`, nor a pull request inside a deeper path.
  const at = literal.indexOf(text);
  if (at >= 0) {
    const bounded = (ch) => ch === undefined || !HANDLE_CHAR.test(ch);
    if (bounded(literal[at - 1]) && bounded(literal[at + text.length])) {
      return true;
    }
  }

  /*
   * As a sentence spells it: the same parts, in order, as whole words. A
   * hyphenated token stays whole, so `phase-1-inventory-v2` is still a
   * different handle rather than this one followed by a stray word.
   *
   * Spelled out, a handle is held to what a title is held to. Written as
   * written, `in-review` in a note is a reference somebody typed; written as
   * "in review" it is the two commonest words in any sentence about state,
   * and "leave this in review until Friday" is not about that step.
   */
  if (!identifies(parts)) return false;
  return tokens.some(
    (_, index) =>
      index + parts.length <= tokens.length &&
      parts.every((part, offset) => tokens[index + offset] === part),
  );
}
