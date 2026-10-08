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
import {
  expectationHoursFor,
  freshnessKindForPhase,
  freshnessModel,
} from "./freshness.js";
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
  overdue: { tone: "warn", glyph: "▲" },
  stale: { tone: "warn", glyph: "◷" },
  stalled: { tone: "warn", glyph: "◷" },
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
  "overdue",
  "stale",
  "stalled",
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
 * States that mean something is wrong, as opposed to in hand.
 *
 * These are the risk overrides: core computes a card's state from its phase
 * and replaces it with one of these only when the card is blocked, overdue or
 * has gone quiet. They are what the urgent band lists, and what keeps a card
 * out of a collapsed fold — one definition, so the band and the fold cannot
 * disagree about which cards a reader must not miss.
 */
export const ATTENTION_STATES = Object.freeze([
  "blocked",
  "at_risk",
  "stale",
  /*
   * The same two risks under the words the specification uses for them.
   * Core's tokens are `at_risk` (its reason is "overdue or due within 24
   * hours") and `stale`; reading both spellings costs nothing and is the
   * difference between an overdue card leading the dashboard and collapsing
   * out of it.
   */
  "overdue",
  "stalled",
]);

/** Is this card asking for someone's attention? */
export function needsAttention(summary) {
  return ATTENTION_STATES.includes(asText(summary?.status?.state));
}

/**
 * What a hint is called.
 *
 * Hints are not statuses. `no_plan` used to be one, and a planless card then
 * read "No plan" in the Tasks table's Status column — for most of the tasks
 * in it, since most tasks have no plan. Core computes the state from the
 * phase now and says "this has no plan" separately, as a note a surface can
 * show where a plan is expected and skip where it is noise.
 *
 * The vocabulary is open: a hint this client has never seen reads as its own
 * token with the underscores taken out, which is the same forward-compatible
 * rule the status labels follow.
 */
const HINT_LABELS = Object.freeze({
  no_plan: "No plan",
});

/**
 * The hints a row carries, in core's order, named.
 *
 * A hint is a token, but an open vocabulary with labels is the obvious thing
 * for core to grow into, so an entry that arrives as `{key, label}` is read
 * too. Anything else is dropped rather than stringified: `String({})` is
 * `"[object Object]"`, which would have rendered as a note *and* made the
 * Overview's planless fold match nothing.
 */
function hintParts(raw) {
  const seen = new Set();
  return (Array.isArray(raw) ? raw : [])
    .map((entry) => (typeof entry === "string" ? entry : entry?.key))
    .map(asText)
    .filter((key) => key && !seen.has(key) && seen.add(key))
    .map((key) => ({
      key,
      label: HINT_LABELS[key] || key.replaceAll("_", " "),
    }));
}

/** Does this row carry a given hint? */
export function hasHint(summary, key) {
  return (summary?.hints ?? []).some((hint) => hint.key === key);
}

/**
 * The label a state reads as when core sent none — the legacy path, and
 * `summaryFromStatus` callers that have a state and a reason only.
 */
const FALLBACK_LABELS = Object.freeze({
  blocked: "Blocked",
  at_risk: "At risk",
  overdue: "Overdue",
  stale: "Stale",
  stalled: "Stale",
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

/**
 * @param {object|null|undefined} raw the `status` or `set_status` part
 * @param {string} [sourceStatus] the source's own word for this state
 *
 * A state Nexus has no name for reads better in the tracker's own words than
 * as its raw token: "Custom waiting state", not "vendor waiting".
 *
 * But only over a *mechanical* label. Core labels a state it does not know by
 * replacing the underscores (`summaryLabel`), which carries nothing the token
 * did not — that is the one this replaces. A label core chose deliberately is
 * core's answer and stands, which is what keeps a state this client has never
 * seen rendering core's wording rather than the client's guess at it.
 */
function statusPart(raw, sourceStatus = "") {
  const state = asText(raw?.state);
  // "Unknown" counts as unnamed: it is the one label of ours that carries
  // less than the source's own word does.
  const named = state !== "unknown" && Boolean(FALLBACK_LABELS[state]);
  const given = asText(raw?.label);
  const mechanical = !given || given === state.replaceAll("_", " ");
  const label =
    (!named && mechanical && asText(sourceStatus)) ||
    given ||
    stateLabel(state);
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

/**
 * @param {object|null|undefined} raw the `progress` part
 * @param {boolean} [unresolved] `resolution_truncated`: linked plan or child
 *   work that could not be resolved at all. It makes `done` a lower bound in
 *   exactly the same way `progress.truncated` does — core's plan branch sets
 *   one without the other — so it marks the count rather than hiding in a
 *   tooltip the Overview showed and the Tasks row did not.
 */
/**
 * Is the source's word worth a badge of its own?
 *
 * Not when the status label already *is* that word — which is what happens
 * for a state Nexus cannot name — and not when it differs only in case.
 */
function sourceWordAdds(sourceStatus, status) {
  const word = asText(sourceStatus);
  if (!word) return false;
  return word.toLocaleLowerCase() !== asText(status?.label).toLocaleLowerCase();
}

function progressPart(raw, unresolved = false) {
  const total = Number(raw?.total);
  if (!Number.isFinite(total) || total <= 0) return null;
  const done = Math.max(0, Math.min(Number(raw?.done) || 0, total));
  const unit = asText(raw?.unit) || "steps";
  const truncated = raw?.truncated === true || unresolved === true;
  /*
   * `3+/7`, not `3/7+`, when linked work could not all be read. The marker
   * belongs to the number that is a lower bound: the total is exact in both
   * of core's branches — the plan's step count, or the number of explicit
   * children — and only `done` can be undercounted. `3/7+` read as "more
   * than seven".
   */
  const mark = truncated ? "+" : "";
  return {
    done,
    total,
    unit,
    truncated,
    percent: Math.round((done / total) * 100),
    /** `3/7`, the same text at every density. */
    count: `${done}${mark}/${total}`,
    /** `3/7 steps`, where there is room for the unit. */
    label: `${done}${mark}/${total} ${unit}`,
    /** The sentence a badge uses for its accessible name. */
    sentence: `${truncated ? "at least " : ""}${done} of ${total} ${unit} done`,
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
    /*
     * The instant the oldest ask fired, which is what an age on screen has
     * to be computed from. `oldest_age` is seconds measured at read time and
     * core excludes it from change detection for that reason; a page left
     * open would keep reporting the age it had when it loaded.
     */
    oldestAt: asText(raw?.oldest_at),
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

/**
 * Just the computed state, for a caller that needs to sort or count rows
 * rather than render them.
 *
 * The whole model resolves freshness, step digests and plan geometry; a
 * count over two thousand rows does not need any of it. The vocabulary is
 * the same one `workSummaryModel` uses, so a count and the rows it counts
 * cannot disagree.
 *
 * @returns {string} the state, or "" when nothing at all is knowable
 */
export function statusStateOf(row) {
  const raw = rawWorkSummary(row);
  /*
   * The same branch `fromComputed` takes, and through the same reader: on the
   * presence of `status`, not on its contents. Branching on a non-empty
   * `state` instead let a malformed `status` fall through to the stored
   * phase here while the renderer showed Unknown — so a card filed done was
   * folded into Closed work while reading "Unknown".
   */
  if (raw?.status) {
    // `?? "unknown"` on the part, not `|| "unknown"` on the state: a status
    // with a label and no state renders an empty state, and the shortcut has
    // to render the same nothing rather than guessing Unknown.
    const part = statusPart(raw.status);
    return part ? part.state : "unknown";
  }
  const health = planHealthModel({
    ...row,
    health:
      row?.health ??
      (row?.plan_state?.health ? { status: row.plan_state.health } : undefined),
  });
  // Mirrors `fromLegacy` exactly, including its last resort: a row with no
  // health and no phase is Unknown, not blank, so a count and a rendered row
  // never disagree about what a bare row is.
  return (
    (health.known ? health.state : asText(row?.phase ?? row?.column_key)) ||
    "unknown"
  );
}

/**
 * Does the computation say this work is over?
 *
 * Separate from the stored phase on purpose: a card somebody marked done
 * whose plan is still blocked is not finished, and a list that folds it away
 * on the phase alone hides the disagreement.
 */
export function isComputedClosed(row) {
  return (
    CLOSED_STATES.has(statusStateOf(row)) ||
    freshnessKindForPhase("", asText(row?.state ?? row?.lifecycle_state)) ===
      "closed"
  );
}

/**
 * A card's prose body, whichever way the response spells it.
 *
 * `summary=1` puts the computed object at `summary` and moves the prose to
 * `summary_text`; without it the prose stays at `summary`. Anything that
 * renders a card's body has to read both, or a surface that opted in paints
 * `[object Object]` where the body should be.
 */
export function workProse(row) {
  return (
    asText(row?.summary_text) ||
    (typeof row?.summary === "string" ? row.summary.trim() : "")
  );
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
  /*
   * The expectation the gate above resolved, carried so a badge a surface
   * renders is judged against the same number. `freshnessModel` reads a
   * projection-supplied `update_expectation_hours` off the row, and a badge
   * re-rendered without the row would show a different expectation from the
   * one that decided its colour.
   */
  const expectationHours = expectationHoursFor(freshnessKind, { row });
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
    expectationHours,
    /**
     * Freshness against the cadence the card's state implies, or null when
     * unknowable — and null for anything finished, which nobody needs
     * prompting about.
     */
    freshness: model.lastMovementAt
      ? freshnessModel(model.lastMovementAt, {
          kind: freshnessKind,
          expectationHours,
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
    hints: [],
    sourceStatus: "",
    sourceStatusShown: false,
    lifecycleState: asText(row?.state ?? row?.lifecycle_state),
    setStatus: null,
    progress: null,
    next: null,
    steps: null,
    owner: "",
    due: "",
    age: null,
    createdAt: "",
    lastMovementAt: "",
    source: null,
    attention: null,
    attentionTruncated: false,
    resolutionTruncated: false,
  };
}

function fromComputed(raw, row, { now }) {
  const sourceStatus = asText(raw.source?.native_status);
  const status = statusPart(raw.status, sourceStatus);
  // No source word for `set_status`: that is the phase the card is filed
  // under in Nexus, and borrowing the tracker's workflow word for it read
  // "Blocked · marked in uat", which is not what the board says.
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
      progress: progressPart(raw.progress, raw.resolution_truncated === true),
      next: nextPart(raw.next),
      steps: stepListsModel(raw.steps, { now }),
      // Core emits a step digest only for a card with a plan, and counts
      // `steps` only then; `cards` counts explicit children instead.
      hasPlan: Boolean(raw.steps) || asText(raw.progress?.unit) === "steps",
      owner: asText(raw.owner),
      due: asText(raw.due),
      age: Number.isFinite(age) && age >= 0 ? age : null,
      /*
       * The creation instant behind `age`. Same reason as the oldest ask:
       * `age` is seconds at read time, so an age rendered from it freezes.
       */
      createdAt: asText(raw.created_at),
      lastMovementAt: asText(raw.last_movement_at),
      source: raw.source && typeof raw.source === "object" ? raw.source : null,
      /*
       * The source's own word for where this stands — "In UAT", "awaiting
       * triage". Core sends `source` only for a non-Nexus authority, exactly
       * so a client can show it beside the computed status rather than
       * instead of it: the two answer different questions, and a phase Nexus
       * has no name for reads better in the source's words than mapped to the
       * nearest one we do have.
       */
      sourceStatus,
      /*
       * Whether the word is worth a badge of its own. When the computed state
       * had no Nexus name the label above already *is* the source's word, and
       * a badge beside it would say the same thing twice.
       */
      sourceStatusShown: sourceWordAdds(sourceStatus, status),
      /*
       * A core between the two contracts still reports "no plan" where the
       * status goes, so it is read back as the hint it has become. One rule
       * for "does this card have a plan", whichever wire shape answered.
       */
      hints: hintParts(
        Array.isArray(raw.hints) || status?.state !== "no_plan"
          ? raw.hints
          : ["no_plan"],
      ),
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
  // Core gates the computed part to non-Nexus authorities; match that, so a
  // Nexus card does not grow a badge the computed path never gives it.
  const sourceStatus =
    asText(row?.source?.authority).toLowerCase() === "nexus"
      ? ""
      : asText(row?.source?.native_status);
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
    ? statusPart(
        {
          state: health.state,
          label: health.label,
          reason: health.reason,
          since: health.since,
        },
        sourceStatus,
      )
    : statusPart(phase ? { state: phase } : { state: "unknown" }, sourceStatus);
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
      progress: progressPart(
        { ...rawProgress, unit: "steps" },
        row?.plan_resolution_truncated === true,
      ),
      hasPlan: Array.isArray(row?.plan_state?.steps)
        ? row.plan_state.steps.length > 0
        : Boolean(row?.plan_state),
      /*
       * Same rule for a core with no computed summary at all — and only that
       * rule. `hasPlan` is false for every list row from such a core, since
       * list rows carry no plan state at all, so deriving the hint from it
       * would put "No plan" on every row in the table.
       */
      hints: hintParts(status?.state === "no_plan" ? ["no_plan"] : null),
      // `nextStepModel` calls the remainder `extra`; the contract calls it
      // `more`, and one renderer can only read one name.
      next: nextPart(legacyNext(row)),
      steps: stepListsModel(row?.plan_step_digest, { now }),
      owner: asText(row?.owner),
      due: asText(row?.due_at),
      createdAt: asText(row?.created_at),
      lastMovementAt:
        asText(row?.plan_state?.last_movement_at) || asText(row?.updated_at),
      source: row?.source && typeof row.source === "object" ? row.source : null,
      sourceStatus,
      sourceStatusShown: sourceWordAdds(sourceStatus, status),
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
  const carried = rawWorkSummary(row);
  /*
   * An override is a fresher computation of the same card, not a different
   * card. One that says nothing about hints is not saying the card has none,
   * so the row's survive — the plan read the task header prefers does not
   * compute them, and dropping them there would silence a note on the one
   * surface with room for it.
   */
  const raw = override
    ? {
        ...(carried?.hints && !override.hints ? { hints: carried.hints } : {}),
        ...override,
      }
    : carried;
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
