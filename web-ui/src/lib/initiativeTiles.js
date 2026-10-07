/**
 * The Overview initiative tiles.
 *
 * A tile answers "what is the state and progress of this initiative" in a
 * glance, from `/overview` alone — no per-tile fetch.
 *
 * Everything shown is computed by core and read here as given: `plan_health`
 * (or the older `health`) carries the state and the reason, `plan_state`
 * carries progress, the critical path and the next step, `next_step` names it,
 * and `geometry` carries the bounded graph a mini-viz is drawn from —
 * `{shape, nodes: [{id, status, layer, after}], total_nodes,
 * collapsed_nodes}`, capped at 24 nodes with the remainder counted.
 *
 * Two things the tile does rather than shows raw:
 *
 * - The description is a **plain-text excerpt**. `summary` is authored
 *   markdown, so a tile rendering it verbatim read `**Goal:** ship the…`.
 * - Tiles are **sorted by attention** and grouped, because a dashboard's job
 *   is to put what is wrong at the top. The order lives in `planHealth.js` so
 *   a tile and a page header cannot disagree about which is worse.
 */

import { freshnessKindForPhase, freshnessModel } from "./freshness.js";
import { markdownExcerpt } from "./markdown.js";
import { nextStepModel, planHealthModel } from "./planHealth.js";

const SHAPE_LABELS = Object.freeze({
  chain: "Timeline",
  dag: "Tech tree",
  lanes: "Lanes",
});

const asText = (value) => String(value ?? "").trim();

/**
 * Which block of the Overview a tile belongs to.
 *
 * Finished and planless initiatives are real but are not what a dashboard is
 * for, so they collapse at the bottom instead of pushing live work down.
 */
export const TILE_GROUPS = Object.freeze({
  ATTENTION: "attention",
  DONE: "done",
  NO_PLAN: "no_plan",
});

export function tileGroup(state) {
  if (state === "done") return TILE_GROUPS.DONE;
  if (state === "no_plan" || !state) return TILE_GROUPS.NO_PLAN;
  return TILE_GROUPS.ATTENTION;
}

/**
 * Segments for the tile's progress bar: one per step, carrying the status that
 * colours it and whether it sits on the critical path.
 *
 * Core's `geometry` is preferred — it is already bounded to 24 nodes and counts
 * what it omitted — and `plan_state.steps` is the fallback for a projection
 * that carries one without the other.
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
    segments: shown.map((step) => ({
      id: asText(step?.id),
      status: asText(step?.status) || "not_started",
      onCriticalPath: critical.has(asText(step?.id)),
    })),
    overflow: omitted,
  };
}

/**
 * One tile.
 *
 * @param {object} item a live initiatives projection row
 * @param {{ now?: number, href?: (ref: string) => string, excerptLimit?: number }} [options]
 */
export function initiativeTileModel(item, options = {}) {
  const { href = () => "", excerptLimit = 120, now = Date.now() } = options;
  const ref = asText(item?.ref);
  const planState = item?.plan_state ?? null;
  const geometry = item?.geometry ?? null;

  const health = planHealthModel(item);

  // The plan's own progress wins over the row's. Core already copies one onto
  // the other, but if they ever disagree the bar is drawn from the plan's steps
  // and the count beside it has to describe the same thing.
  const rawProgress =
    Number(planState?.progress?.total) > 0
      ? planState.progress
      : item?.progress;
  const progress =
    Number(rawProgress?.total) > 0
      ? {
          done: Number(rawProgress.done) || 0,
          total: Number(rawProgress.total),
        }
      : null;

  // Blocked step titles, which the projection does carry. They are the sharpest
  // thing a tile can say, so they outrank the next step when present.
  const needs = (Array.isArray(item?.needs) ? item.needs : [])
    .map(asText)
    .filter(Boolean);

  const movedAt =
    asText(planState?.last_movement_at) || asText(item?.updated_at);

  const shape = asText(geometry?.shape ?? planState?.shape);
  const bars = planSegments(planState, geometry);
  const next = nextStepModel(item);

  /*
   * An initiative is expected to move every three days, so its age is a
   * freshness badge rather than a bare "2h": green inside the expectation,
   * amber up to twice it, red beyond.
   *
   * Unless it is finished. A freshness badge is a prompt — it says somebody
   * should look — and a done initiative is not asking anyone for anything, so
   * expanding the Done fold and finding a red "9d, expected every 3d" is the
   * badge telling a reader to chase work that is already delivered. Finished
   * is `closed`, which `freshnessModel` gives no badge at all.
   *
   * Three things can say it is finished and they are read in the order they
   * are trustworthy: the card's lifecycle (archived or trashed), its own
   * phase, and the computed health state. Core can mark health `done` while a
   * card's phase still lags, and the reverse; either one is enough.
   */
  const closed =
    health.state === "done" ||
    freshnessKindForPhase(item?.phase, item?.state ?? item?.lifecycle_state) ===
      "closed";
  const freshness = freshnessModel(movedAt, {
    kind: closed ? "closed" : "initiative",
    row: item,
    verb: "moved",
    now,
  });

  /*
   * Stale is what the freshness badge is for. Showing core's "Stale" pill
   * beside a red `9d` says the same thing twice and costs the title a line of
   * width — the whole reason the badges moved off it. Every other health
   * state (Blocked, At risk, On track) is something the age cannot express,
   * so it keeps its pill.
   *
   * Only when the badge actually reads late, though. Core computes staleness
   * its own way and can call an initiative stale while its last movement is
   * well inside our expectation; dropping the pill there would replace the
   * one surface saying something is wrong with a green badge saying it is
   * fine. When the two disagree, the reader gets both.
   */
  const showHealth =
    health.known &&
    !(health.state === "stale" && freshness && freshness.state !== "fresh");

  return {
    ref,
    title: asText(item?.title) || ref,
    href: href(ref),
    // A plain line, never the markdown source.
    excerpt: markdownExcerpt(item?.summary, { limit: excerptLimit }),
    phase: asText(item?.phase).replaceAll("_", " "),
    priority: asText(item?.priority),
    health,
    rank: health.rank,
    group: tileGroup(health.state),
    progress,
    shape,
    shapeLabel: SHAPE_LABELS[shape] ?? "",
    hasPlan: Boolean(planState),
    ...bars,
    next,
    needs,
    /** ISO instant for the age badge; the badge owns the wording. */
    movedAt,
    /**
     * Freshness against the initiative cadence, or null when unknowable — and
     * null for anything finished, which nobody needs prompting about.
     */
    freshness,
    /** The expectation this tile is judged against, for the badge to echo. */
    freshnessKind: closed ? "closed" : "initiative",
    /** False when the freshness badge already says what health would. */
    showHealth,
  };
}

/**
 * Tiles for the grid, worst first.
 *
 * Within a health state the projection's own order is kept — core sorts by
 * priority or recency, and re-sorting it here would throw that away. The sort
 * is stable, so equal ranks come out in the order they arrived.
 *
 * @param {object[]} items
 * @param {{ now?: number, href?: (ref: string) => string }} [options]
 */
export function initiativeTiles(items = [], options = {}) {
  return (Array.isArray(items) ? items : [])
    .filter((item) => asText(item?.ref))
    .map((item) => initiativeTileModel(item, options))
    .sort((a, b) => a.rank - b.rank);
}

/**
 * The tiles, split into the blocks the Overview renders.
 *
 * @param {object[]} items
 * @param {{ now?: number, href?: (ref: string) => string }} [options]
 */
export function groupedInitiativeTiles(items = [], options = {}) {
  const tiles = initiativeTiles(items, options);
  return {
    attention: tiles.filter((tile) => tile.group === TILE_GROUPS.ATTENTION),
    done: tiles.filter((tile) => tile.group === TILE_GROUPS.DONE),
    noPlan: tiles.filter((tile) => tile.group === TILE_GROUPS.NO_PLAN),
  };
}

/**
 * The single Inbox line the Overview is allowed.
 *
 * The brief is explicit that a dashboard must not restate the Inbox: at most
 * one link saying how much is waiting, plus a pill on the tile it belongs to.
 * This replaces a per-item list.
 *
 * It says "items", not "decisions". Core's `needs_you` count mixes decisions
 * with human-assigned and blocked tasks and other Inbox entries, so calling
 * them all decisions would claim something is waiting for an answer when it is
 * a task waiting to be picked up.
 *
 * @param {{ status?: string, count?: number, truncated?: boolean, href?: string }} needsYou
 */
export function inboxWaitingLine(needsYou) {
  if (!needsYou || needsYou.status !== "ok") return null;
  const count = Number(needsYou.count) || 0;
  if (count <= 0) return null;
  const suffix = needsYou.truncated ? "+" : "";
  return {
    count,
    href: asText(needsYou.href),
    label: `${count}${suffix} ${count === 1 ? "item needs" : "items need"} you`,
  };
}
