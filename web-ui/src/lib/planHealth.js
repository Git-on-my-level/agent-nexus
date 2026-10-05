/**
 * Plan health, as one vocabulary.
 *
 * Core computes health; this normalizes it so every surface says the same
 * thing in the same order. Two shapes arrive:
 *
 * - `plan_health {state, reason, since}` — the computed field, where `state`
 *   is one of `no_plan | stale | blocked | at_risk | on_track | done`.
 * - `health {status, reason}` — the older projection field, whose `status` is
 *   one of `on_track | stalled | blocked`.
 *
 * The older field is read as the newer one (`stalled` is `stale`), and the two
 * states it cannot express — `done` and `no_plan` — are derived from the plan
 * progress and the presence of a plan. Nothing else is inferred: a row with no
 * health at all gets no badge rather than an invented "on track".
 *
 * `ATTENTION_ORDER` is the sort the Overview uses, and the reason the order is
 * here rather than in the page: a tile, a list row and a page header must not
 * disagree about which of two initiatives is more urgent.
 */

const asText = (value) => String(value ?? "").trim();

/** Worst first. This is the Overview's initiative sort. */
export const ATTENTION_ORDER = Object.freeze([
  "blocked",
  "at_risk",
  "stale",
  "on_track",
  "done",
  "no_plan",
]);

/**
 * What each state is called, short and long.
 *
 * `short` is what a compact badge shows and is deliberately never truncated —
 * that is the whole point of having one. `label` is the full text, which rides
 * along in the `title` and the accessible name.
 */
export const PLAN_HEALTH = Object.freeze({
  blocked: {
    state: "blocked",
    label: "Blocked",
    short: "Blocked",
    glyph: "■",
    tone: "danger",
  },
  at_risk: {
    state: "at_risk",
    label: "At risk",
    short: "At risk",
    glyph: "▲",
    tone: "warn",
  },
  stale: {
    state: "stale",
    label: "Stale",
    short: "Stale",
    glyph: "◷",
    tone: "warn",
  },
  on_track: {
    state: "on_track",
    label: "On track",
    short: "On track",
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
  no_plan: {
    state: "no_plan",
    label: "No plan",
    short: "No plan",
    glyph: "–",
    tone: "neutral",
  },
});

/** Legacy `health.status` values, as plan-health states. */
const LEGACY_STATUS = Object.freeze({
  on_track: "on_track",
  stalled: "stale",
  stale: "stale",
  blocked: "blocked",
  at_risk: "at_risk",
  done: "done",
  no_plan: "no_plan",
});

export function attentionRank(state) {
  const index = ATTENTION_ORDER.indexOf(asText(state));
  // An unknown state sorts with the planless tail rather than above blocked.
  return index === -1 ? ATTENTION_ORDER.length : index;
}

function progressOf(source) {
  const done = Number(source?.done);
  const total = Number(source?.total);
  if (!Number.isFinite(done) || !Number.isFinite(total) || total <= 0) {
    return null;
  }
  return { done: Math.max(0, Math.min(done, total)), total };
}

/**
 * One row's plan health.
 *
 * @param {object} item an initiative/card projection row
 * @param {{ hasPlan?: boolean|null, progress?: {done: number, total: number}|null }} [options]
 *   Overrides for callers that already know the plan state — the initiative
 *   page reads the plan itself, where the Overview reads the projection.
 * @returns {{
 *   state: string, label: string, short: string, glyph: string, tone: string,
 *   reason: string, since: string, rank: number, known: boolean
 * }}
 */
export function planHealthModel(item, options = {}) {
  const planHealth = item?.plan_health ?? null;
  const legacy = item?.health ?? null;

  const hasPlan =
    options.hasPlan ??
    (item?.plan_state ? true : item?.plan_state === null ? false : null);
  const progress =
    options.progress ??
    progressOf(item?.plan_state?.progress) ??
    progressOf(item?.progress);

  // The computed field is authoritative where it exists.
  let state = LEGACY_STATUS[asText(planHealth?.state)] ?? "";
  if (!state) {
    // The older field cannot express `no_plan`: core derives its status from
    // the card's native phase even for an initiative with no plan at all, so
    // its `on_track` there means "nothing is wrong", not "on track". For a
    // planless initiative that reads as No plan, which is where the Overview
    // files it. A legacy `blocked` or `stalled` is a real signal about the
    // card and keeps precedence — a blocked initiative belongs in the urgent
    // band whether or not anyone has written it a plan.
    const fromLegacy = LEGACY_STATUS[asText(legacy?.status ?? legacy)] ?? "";
    state =
      hasPlan === false && (!fromLegacy || fromLegacy === "on_track")
        ? "no_plan"
        : fromLegacy;
  }

  // Finished work outranks the computed status — an initiative whose every
  // step is done is done, whatever the stall clock says — but not a blocked
  // one, where the block is the thing the reader has to see.
  if (progress && progress.done >= progress.total && state !== "blocked") {
    state = "done";
  }

  const known = Boolean(state);
  const definition = PLAN_HEALTH[state] ?? PLAN_HEALTH.no_plan;
  return {
    ...definition,
    state: known ? definition.state : "",
    reason: asText(planHealth?.reason ?? legacy?.reason),
    since: asText(planHealth?.since),
    rank: attentionRank(state),
    known,
  };
}

/**
 * The sentence a health badge puts in its `title` and accessible name: the
 * full label, plus the reason core gave for it when there is one.
 *
 * @param {{ label?: string, reason?: string }} health
 */
export function planHealthTitle(health) {
  const label = asText(health?.label);
  const reason = asText(health?.reason);
  if (!label) return reason;
  return reason ? `${label} — ${reason}` : label;
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

/**
 * A phase as it reads inside a sentence. `in_progress` is already "in
 * progress", so "this task is in in progress" is what naive interpolation
 * gets you.
 */
function phaseWords(phase) {
  const words = asText(phase).replaceAll("_", " ");
  return /^(in |on |at )/.test(words) ? words : `in ${words}`;
}

/**
 * Does the card's own status disagree with what the plan says?
 *
 * A card parked in `done` whose plan still has open steps, or a card in
 * `blocked` whose plan is on track, is worth a quiet hint on the page — not an
 * error, because either one can be legitimately ahead of the other.
 *
 * @param {string} phase the card's phase
 * @param {{state?: string}} health
 * @param {{done: number, total: number}|null} progress
 * @returns {string} the hint, or "" when they agree
 */
export function planStatusMismatch(phase, health, progress) {
  const cardPhase = asText(phase);
  const state = asText(health?.state);
  if (!cardPhase || !state) return "";

  const planDone = Boolean(progress && progress.done >= progress.total);
  if (cardPhase === "done" && !planDone && state !== "done") {
    const remaining = progress ? progress.total - progress.done : 0;
    return remaining
      ? `This task is marked done, but its plan still has ${remaining} open ${remaining === 1 ? "step" : "steps"}.`
      : "This task is marked done, but its plan is not.";
  }
  if (cardPhase === "blocked" && state === "on_track") {
    return "This task is marked blocked, but its plan reads on track.";
  }
  if (cardPhase !== "blocked" && state === "blocked") {
    return `This task is ${phaseWords(cardPhase)}, but its plan is blocked.`;
  }
  if (cardPhase !== "done" && state === "done") {
    return `Every step in the plan is done, but this task is still ${phaseWords(cardPhase)}.`;
  }
  return "";
}
