/**
 * One work summary, for every surface that shows a card.
 *
 * Core computes the summary — `work_summary` on every card-bearing read, and
 * `summary` on a resolved ref — and this normalizes it into the single model
 * `WorkSummary.svelte` renders. Nothing here decides what a card's state is;
 * core already did, from the plan, the linked cards and the stored phase it
 * holds in one bounded read. The job of this module is to give one vocabulary
 * to the parts core sends, so a row in the Tasks table, a card on the board
 * and a card on the Overview cannot disagree about the same task.
 *
 * Two shapes arrive, and both read the same way:
 *
 * - **Computed** (`work_summary`, or `summary` when it is an object): the
 *   contract's `WorkSummary` — `status`, and whichever of `set_status`,
 *   `progress`, `next`, `steps`, `owner`, `due`, `age`, `last_movement_at`,
 *   `source` and `attention` apply. Core omits a part rather than sending an
 *   empty one, so "present" is the whole test; there is no branching on what
 *   kind of card it is.
 * - **Legacy**: a core before this field, whose `plan_health` / `health`,
 *   `progress`, `next_step` and `plan_step_digest` carry the same facts in
 *   separate fields. `planHealth.js` already reads those, so the fallback
 *   composes them into the same model rather than rendering a second way.
 *
 * `status.label` is core's own wording, shown verbatim. A state this client
 * has never seen still renders — it gets a neutral tone and core's label —
 * which is what keeps a core that learns a new state from blanking a card.
 *
 * Two fields are read from the row rather than the summary, and are marked as
 * such below: `segments` (the plan's shape, from the same Overview row's
 * `geometry` / `plan_state`) and `lifecycleState`. They are presentation of
 * data the request already carried, not extra reads.
 */

import { formatAge, ageTitle } from "./ageBadge.js";
import { freshnessKindForPhase, freshnessModel } from "./freshness.js";
import { nextStepModel, planHealthModel } from "./planHealth.js";

const asText = (value) => String(value ?? "").trim();

/** How many steps one digest list shows before the rest become a count. */
export const STEP_LIST_LIMIT = 3;

/** The three lists, in reading order: what landed, what moves, what is next. */
export const STEP_LISTS = Object.freeze([
  { key: "completed", label: "Recently completed" },
  { key: "current", label: "Current" },
  { key: "next", label: "Next" },
]);

const SHAPE_LABELS = Object.freeze({
  chain: "Timeline",
  dag: "Tech tree",
  lanes: "Lanes",
});

/**
 * Tone and glyph per state.
 *
 * Core owns the words; this owns only how loud they are. Computed health
 * states and stored phases share the table because `set_status` carries a
 * phase and is rendered by the same badge. An unlisted state is neutral: a
 * client that invented a colour for a state it does not understand would be
 * guessing at how urgent it is.
 */
const STATUS_PRESENTATION = Object.freeze({
  blocked: { tone: "danger", glyph: "■" },
  at_risk: { tone: "warn", glyph: "▲" },
  stale: { tone: "warn", glyph: "◷" },
  on_track: { tone: "ok", glyph: "●" },
  in_progress: { tone: "ok", glyph: "●" },
  done: { tone: "ok", glyph: "✓" },
  review: { tone: "neutral", glyph: "◆" },
  in_review: { tone: "neutral", glyph: "◆" },
  ready: { tone: "neutral", glyph: "○" },
  backlog: { tone: "neutral", glyph: "○" },
  cancelled: { tone: "neutral", glyph: "×" },
  no_plan: { tone: "neutral", glyph: "–" },
  unknown: { tone: "neutral", glyph: "·" },
});

const NEUTRAL = Object.freeze({ tone: "neutral", glyph: "·" });

/** Worst first. The order every list of cards is sorted by. */
export const ATTENTION_ORDER = Object.freeze([
  "blocked",
  "at_risk",
  "stale",
  "on_track",
  "in_progress",
  "review",
  "in_review",
  "ready",
  "backlog",
  "done",
  "cancelled",
  "no_plan",
]);

export function statusRank(state) {
  const index = ATTENTION_ORDER.indexOf(asText(state));
  // A state this client does not know sorts with the quiet tail rather than
  // above blocked: an unknown word is not evidence of urgency.
  return index === -1 ? ATTENTION_ORDER.length : index;
}

/**
 * A count-by-state chip: `3 Blocked`.
 *
 * A tally over cards, not one card's status, so it is a label and a tone
 * rather than a summary — but it reads from the same table, because a header
 * that called a state something else than the cards under it would be a
 * second vocabulary.
 */
export function statusChip(state, count) {
  const key = asText(state);
  return {
    state: key,
    count: Number(count) || 0,
    label: stateLabel(key),
    tone: (STATUS_PRESENTATION[key] ?? NEUTRAL).tone,
    rank: statusRank(key),
  };
}

/** States that mean the work is over, and nothing is being asked of anyone. */
const CLOSED_STATES = new Set(["done", "cancelled"]);

/**
 * The label a state reads as when core sent none — the legacy path, and
 * `summaryFromStatus` callers that have a state and a reason only.
 */
const FALLBACK_LABELS = Object.freeze({
  blocked: "Blocked",
  at_risk: "At risk",
  stale: "Stale",
  on_track: "In progress",
  in_progress: "In progress",
  review: "In review",
  in_review: "In review",
  ready: "Ready",
  backlog: "Backlog",
  done: "Done",
  cancelled: "Cancelled",
  no_plan: "No plan",
  unknown: "Unknown",
});

function stateLabel(state) {
  const key = asText(state);
  return FALLBACK_LABELS[key] || key.replaceAll("_", " ");
}

function statusPart(raw) {
  const state = asText(raw?.state);
  const label = asText(raw?.label) || stateLabel(state);
  if (!state && !label) return null;
  return {
    state,
    label,
    reason: asText(raw?.reason),
    since: asText(raw?.since),
    rank: statusRank(state),
    ...(STATUS_PRESENTATION[state] ?? NEUTRAL),
  };
}

function progressPart(raw) {
  const total = Number(raw?.total);
  if (!Number.isFinite(total) || total <= 0) return null;
  const done = Math.max(0, Math.min(Number(raw?.done) || 0, total));
  const unit = asText(raw?.unit) || "steps";
  return {
    done,
    total,
    unit,
    truncated: raw?.truncated === true,
    percent: Math.round((done / total) * 100),
    /** `3/7`, the same text at every density. */
    count: `${done}/${total}`,
    /** `3/7 steps`, where there is room for the unit. */
    label: `${done}/${total} ${unit}`,
  };
}

/** The legacy next step, under the contract's field names. */
function legacyNext(row) {
  const next = nextStepModel(row);
  return next ? { ...next, more: next.extra } : null;
}

function nextPart(raw) {
  const title = asText(raw?.title);
  const id = asText(raw?.id);
  if (!title && !id) return null;
  return {
    id,
    title: title || id,
    ref: asText(raw?.ref),
    more: Math.max(0, Number(raw?.more) || 0),
  };
}

function attentionPart(raw) {
  const count = Number(raw?.count);
  if (!Number.isFinite(count) || count <= 0) return null;
  const oldest = Number(raw?.oldest_age);
  return {
    count,
    oldestAge: Number.isFinite(oldest) && oldest >= 0 ? oldest : null,
    truncated: raw?.truncated === true,
    /** `2 asks`, with a `+` when the sampled window was full. */
    label: `${count}${raw?.truncated === true ? "+" : ""} ${count === 1 ? "ask" : "asks"}`,
  };
}

function stepRows(key, source, limit, now) {
  const rows = Array.isArray(source?.items) ? source.items : [];
  const shown = rows.slice(0, limit);
  const counted = Math.max(0, Number(source?.more) || 0);
  return {
    items: shown.map((step, index) => {
      const at = asText(step?.at);
      return {
        id: asText(step?.id),
        /*
         * A row's identity for keyed rendering. Core gives every step a unique
         * id, but a payload that repeats one (or omits it) must still render
         * rather than throwing `each_key_duplicate` and taking the page down.
         */
        key: [key, asText(step?.id), index].join("#"),
        // A step with no title is a step id; showing the id beats showing
        // nothing, and core already requires a title on every written step.
        title: asText(step?.title) || asText(step?.id),
        status: asText(step?.status) || "not_started",
        at,
        /** `2d` for a completed step; empty where there is no instant. */
        age: at ? formatAge(at, now) : "",
        /*
         * "Moved", not "finished". The instant core sends is the linked
         * card's last movement — the best completion time it holds — but a
         * comment on a finished card moves it too, so the tooltip must not
         * claim more precision than that.
         */
        ageTitle: at ? ageTitle(at, "moved", now) : "",
      };
    }),
    // Core counts what it omitted; the client's own cut adds to that count
    // rather than replacing it, so "+2 more" stays true if the caps differ.
    more: counted + Math.max(0, rows.length - shown.length),
  };
}

/**
 * The three short step lists, from core's bounded step digest.
 *
 * Nothing is recomputed: core decided which steps are recent, in flight and
 * ready, from the plan and the linked cards the same request had loaded. A
 * core with no digest returns `null`, so the card falls back to the single
 * "Next" line rather than rendering three empty headings.
 *
 * @param {object|null|undefined} digest `steps` from the summary
 * @param {{ limit?: number, now?: number }} [options]
 */
export function stepListsModel(digest, options = {}) {
  const { limit = STEP_LIST_LIMIT, now = Date.now() } = options;
  if (!digest || typeof digest !== "object") return null;
  const lists = {};
  for (const { key, label } of STEP_LISTS) {
    lists[key] = { key, label, ...stepRows(key, digest[key], limit, now) };
  }
  return {
    windowHours: Math.max(0, Number(digest.window_hours) || 0),
    ...lists,
    /** The ordered lists that actually have rows. */
    groups: STEP_LISTS.map(({ key }) => lists[key]).filter(
      (list) => list.items.length > 0,
    ),
  };
}

/**
 * Segments for a card's progress bar: one per step, carrying the status that
 * colours it and whether it sits on the critical path.
 *
 * This is the plan's geometry, not a summary part — the Overview row carries
 * `geometry` (already bounded to 24 nodes, counting what it omitted) and
 * `plan_state` beside the summary, and a card that has neither draws a plain
 * bar from `progress` instead.
 */
export function planSegments(planState, geometry = null, limit = 24) {
  const critical = new Set(
    Array.isArray(planState?.critical_path) ? planState.critical_path : [],
  );
  const nodes = Array.isArray(geometry?.nodes) ? geometry.nodes : null;
  const steps =
    nodes ?? (Array.isArray(planState?.steps) ? planState.steps : []);
  const shown = steps.slice(0, limit);
  const omitted = nodes
    ? Number(geometry?.collapsed_nodes) || 0
    : Math.max(0, steps.length - shown.length);
  return {
    segments: shown.map((step, index) => ({
      id: asText(step?.id),
      // Keyed rendering must survive a payload that repeats or omits an id.
      key: [asText(step?.id), index].join("#"),
      status: asText(step?.status) || "not_started",
      onCriticalPath: critical.has(asText(step?.id)),
    })),
    overflow: omitted,
  };
}

/** The computed summary a row carries, in either of its two spellings. */
export function rawWorkSummary(row) {
  if (!row || typeof row !== "object") return null;
  if (row.work_summary && typeof row.work_summary === "object") {
    return row.work_summary;
  }
  // `summary=1` aliases the object onto `summary`; a resolved ref always does.
  if (
    row.summary &&
    typeof row.summary === "object" &&
    !Array.isArray(row.summary)
  ) {
    return row.summary;
  }
  return null;
}

/** True when this row was answered by a core that computes summaries. */
export function hasWorkSummary(row) {
  return Boolean(rawWorkSummary(row)?.status);
}

function finish(model, row, { now }) {
  const closed =
    CLOSED_STATES.has(model.status?.state) ||
    freshnessKindForPhase("", model.lifecycleState) === "closed";
  /*
   * Which cadence the age is judged against.
   *
   * Finished work gets no badge at all. Planned work is held to the
   * three-day initiative cadence, because a plan is a commitment to a
   * sequence and the thing a reader wants to know is whether the sequence is
   * moving. Everything else is held to the cadence its stored phase implies —
   * a card in progress daily, one in a backlog every fortnight.
   *
   * This is deliberately a property of the summary rather than of the page:
   * the Overview card and the Tasks row for one card must colour the same age
   * the same way, and before this they did not even read the same field.
   */
  const freshnessKind = closed
    ? "closed"
    : model.hasPlan
      ? "initiative"
      : freshnessKindForPhase(model.phaseHint, model.lifecycleState);
  const geometry = row?.geometry ?? null;
  const planState = row?.plan_state ?? null;
  const bars =
    geometry || planState
      ? planSegments(planState, geometry)
      : { segments: [], overflow: 0 };
  const shape = asText(geometry?.shape ?? planState?.shape);
  return {
    ...model,
    closed,
    freshnessKind,
    /**
     * Freshness against the cadence the card's state implies, or null when
     * unknowable — and null for anything finished, which nobody needs
     * prompting about.
     */
    freshness: model.lastMovementAt
      ? freshnessModel(model.lastMovementAt, {
          kind: freshnessKind,
          row,
          verb: "moved",
          now,
        })
      : null,
    ...bars,
    shape,
    shapeLabel: SHAPE_LABELS[shape] ?? "",
  };
}

function baseModel(row) {
  return {
    phaseHint: "",
    hasPlan: false,
    lifecycleState: asText(row?.state ?? row?.lifecycle_state),
    setStatus: null,
    progress: null,
    next: null,
    steps: null,
    owner: "",
    due: "",
    age: null,
    lastMovementAt: "",
    source: null,
    attention: null,
    attentionTruncated: false,
    resolutionTruncated: false,
  };
}

function fromComputed(raw, row, { now }) {
  const status = statusPart(raw.status);
  const setStatus = statusPart(raw.set_status);
  const age = Number(raw.age);
  return finish(
    {
      ...baseModel(row),
      known: Boolean(status),
      computed: true,
      status: status ?? statusPart({ state: "unknown" }),
      setStatus,
      // Where core says the two agree it sends no `set_status`, so the stored
      // phase is the computed state; the freshness expectation needs one of
      // the two and neither is more true than the other.
      phaseHint:
        asText(setStatus?.state) ||
        asText(row?.phase ?? row?.column_key) ||
        asText(status?.state),
      progress: progressPart(raw.progress),
      next: nextPart(raw.next),
      steps: stepListsModel(raw.steps, { now }),
      // Core emits a step digest only for a card with a plan, and counts
      // `steps` only then; `cards` counts explicit children instead.
      hasPlan: Boolean(raw.steps) || asText(raw.progress?.unit) === "steps",
      owner: asText(raw.owner),
      due: asText(raw.due),
      age: Number.isFinite(age) && age >= 0 ? age : null,
      lastMovementAt: asText(raw.last_movement_at),
      source: raw.source && typeof raw.source === "object" ? raw.source : null,
      attention: attentionPart(raw.attention),
      attentionTruncated: raw.attention_truncated === true,
      resolutionTruncated: raw.resolution_truncated === true,
    },
    row,
    { now },
  );
}

/**
 * The same model from a core that computes no summary.
 *
 * Every value here comes from a field that core has published all along, read
 * through `planHealth.js` so the fallback and the computed path cannot
 * disagree about what `stalled` means. `set_status` has no legacy spelling —
 * a core that does not compute health cannot say the stored phase disagrees
 * with it — so the stored phase becomes the status when nothing else is
 * known, which is what the Tasks table showed before this field existed.
 */
function fromLegacy(row, { now }) {
  /*
   * `plan_state.health` is the oldest spelling of all and the one a plan read
   * still returns, so it is normalized into the field `planHealth.js` reads
   * rather than left for each page to map — which is how the task page ended
   * up doing it on its own.
   */
  const health = planHealthModel({
    ...row,
    health:
      row?.health ??
      (row?.plan_state?.health ? { status: row.plan_state.health } : undefined),
  });
  const phase = asText(row?.phase ?? row?.column_key);
  const status = health.known
    ? statusPart({
        state: health.state,
        label: health.label,
        reason: health.reason,
        since: health.since,
      })
    : statusPart(
        phase
          ? { state: phase }
          : // A source can report a state Nexus has no name for; its own
            // words beat printing nothing.
            { state: "unknown", label: asText(row?.source?.native_status) },
      );
  const rawProgress =
    Number(row?.plan_state?.progress?.total) > 0
      ? row.plan_state.progress
      : row?.progress;
  return finish(
    {
      ...baseModel(row),
      known: Boolean(health.known || phase),
      computed: false,
      status,
      // A legacy core never says the phase disagrees, so showing it beside
      // the computed health would be this client's own claim.
      setStatus:
        health.known && phase && phase !== health.state
          ? statusPart({ state: phase })
          : null,
      phaseHint: phase || asText(status?.state),
      progress: progressPart({ ...rawProgress, unit: "steps" }),
      hasPlan: Array.isArray(row?.plan_state?.steps)
        ? row.plan_state.steps.length > 0
        : Boolean(row?.plan_state),
      // `nextStepModel` calls the remainder `extra`; the contract calls it
      // `more`, and one renderer can only read one name.
      next: nextPart(legacyNext(row)),
      steps: stepListsModel(row?.plan_step_digest, { now }),
      owner: asText(row?.owner),
      due: asText(row?.due_at),
      lastMovementAt:
        asText(row?.plan_state?.last_movement_at) || asText(row?.updated_at),
      source: row?.source && typeof row.source === "object" ? row.source : null,
      resolutionTruncated: row?.plan_resolution_truncated === true,
    },
    row,
    { now },
  );
}

/**
 * One card's summary, however the core that answered spells it.
 *
 * `summary` overrides the one on the row, for a caller that read a fresher
 * computation from another endpoint — a plan read recomputes status, progress
 * and the next step, so the task page's header prefers that one over the card
 * row it painted first.
 *
 * @param {object|null|undefined} row a card, work, overview initiative or
 *   resolved-ref row
 * @param {{ now?: number, summary?: object|null }} [options]
 */
export function workSummaryModel(row, options = {}) {
  const { now = Date.now(), summary = null } = options;
  const override =
    summary && typeof summary === "object" && summary.status ? summary : null;
  const raw = override ?? rawWorkSummary(row);
  if (raw?.status) return fromComputed(raw, row, { now });
  return fromLegacy(row, { now });
}

/**
 * The same model from a state and a reason alone.
 *
 * The morning brief's rows carry `{state, reason, since, progress}` rather
 * than a card — core ranked them from the same computation — and they must
 * render through the one status renderer rather than a second badge.
 *
 * @param {{state?: string, label?: string, reason?: string, since?: string,
 *   progress?: object|null}} input
 */
export function summaryFromStatus(input, options = {}) {
  const { now = Date.now() } = options;
  const status = statusPart(input);
  if (!status?.state) return null;
  return finish(
    {
      ...baseModel(null),
      known: true,
      computed: true,
      status,
      progress: progressPart({ ...(input?.progress ?? {}), unit: "steps" }),
      phaseHint: status.state,
    },
    null,
    { now },
  );
}
