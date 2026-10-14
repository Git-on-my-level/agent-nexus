/**
 * Plan health, for a core that computes no card summary.
 *
 * `workSummary.js` is the one status vocabulary, and `WorkSummary.svelte` the
 * one renderer. This reads the separate `plan_health` field a row carries when
 * it has no `work_summary`:
 *
 * - `plan_health {state, reason, since}` — where `state` is one of
 *   `past_promise | moving | no_promise | done`, computed by core from the
 *   promise against the evidence. `reason` is time-independent; how long a
 *   promise has been past is `past_promise_for_seconds`.
 *
 * Nothing else is inferred: a row with no plan health gets no badge rather
 * than an invented state.
 */

const asText = (value) => String(value ?? "").trim();

/** The shared order: past promise first, then no promise, moving, done. */
export const ATTENTION_ORDER = Object.freeze([
  "past_promise",
  "no_promise",
  "moving",
  "done",
]);

/**
 * What each state is called, short and long.
 *
 * `short` is what a compact badge shows and is deliberately never truncated.
 * `label` is the full text, which rides along in the `title` and the
 * accessible name.
 */
export const PLAN_HEALTH = Object.freeze({
  past_promise: {
    state: "past_promise",
    label: "Past promise",
    short: "Past promise",
    glyph: "▲",
    tone: "warn",
  },
  no_promise: {
    state: "no_promise",
    label: "No promise",
    short: "No promise",
    glyph: "–",
    tone: "neutral",
  },
  moving: {
    state: "moving",
    label: "Moving",
    short: "Moving",
    glyph: "●",
    tone: "ok",
  },
  done: {
    state: "done",
    label: "Done",
    short: "Done",
    glyph: "✓",
    tone: "ok",
  },
});

export function attentionRank(state) {
  const index = ATTENTION_ORDER.indexOf(asText(state));
  // An unknown state sorts with the tail rather than above past promise.
  return index === -1 ? ATTENTION_ORDER.length : index;
}

/**
 * One row's plan health.
 *
 * @param {object} item an initiative/card projection row
 * @returns {{
 *   state: string, label: string, short: string, glyph: string, tone: string,
 *   reason: string, since: string, rank: number, known: boolean
 * }}
 */
export function planHealthModel(item) {
  const planHealth = item?.plan_health ?? null;
  const state = asText(planHealth?.state);
  const definition = PLAN_HEALTH[state];
  const known = Boolean(definition);
  return {
    ...(definition ?? PLAN_HEALTH.no_promise),
    state: known ? state : "",
    reason: asText(planHealth?.reason),
    since: asText(planHealth?.since),
    rank: attentionRank(state),
    known,
  };
}

/**
 * The next step a surface should name.
 *
 * `next_step {id, title, ref}` is the computed field. Without it, the plan
 * state's `next_steps` carries ids only, and a step id is a lowercase slug by
 * contract — readable enough to name, and the only thing available. A plan the
 * caller has in hand supplies the real title, which always wins.
 *
 * @param {object} item
 * @param {{ plan?: {steps?: object[]}|null }} [options]
 * @returns {{ id: string, title: string, ref: string, extra: number }|null}
 */
export function nextStepModel(item, options = {}) {
  const steps = Array.isArray(options.plan?.steps) ? options.plan.steps : [];
  const titleById = new Map();
  const refById = new Map();
  for (const step of steps) {
    const id = asText(step?.id);
    if (!id) continue;
    if (asText(step?.title)) titleById.set(id, asText(step.title));
    if (asText(step?.ref)) refById.set(id, asText(step.ref));
  }

  const computed = item?.next_step ?? null;
  const queued = Array.isArray(item?.plan_state?.next_steps)
    ? item.plan_state.next_steps.map(asText).filter(Boolean)
    : [];

  const id = asText(computed?.id) || queued[0] || "";
  if (!id && !asText(computed?.title)) return null;

  const title =
    titleById.get(id) || asText(computed?.title) || humanizeStepId(id);
  if (!title) return null;

  return {
    id,
    title,
    ref: asText(computed?.ref) || refById.get(id) || "",
    extra: Math.max(0, queued.length - 1),
  };
}

/**
 * A step id read as prose. Step ids are lowercase slugs by contract
 * (`^[a-z0-9]+(-[a-z0-9]+)*$`), so this is the closest a surface can get to
 * naming a step it has no title for.
 */
export function humanizeStepId(id) {
  const text = asText(id);
  if (!text) return "";
  const words = text.replaceAll("-", " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}
