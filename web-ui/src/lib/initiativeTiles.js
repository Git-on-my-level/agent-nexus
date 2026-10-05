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
 * Segments for the tile's mini-viz: one per step, carrying the status that
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
      layer: Number.isFinite(Number(step?.layer)) ? Number(step.layer) : 0,
      after: Array.isArray(step?.after) ? step.after.map(asText) : [],
      onCriticalPath: critical.has(asText(step?.id)),
    })),
    overflow: omitted,
  };
}

/**
 * The mini-viz, laid out the way the plan's shape asks for.
 *
 * A chain is one track of steps. Lanes are one track per independent run. A
 * tree is a column per dependency layer — core sends `layer` and `after`, so
 * this is the real graph in miniature rather than a bar standing in for one.
 *
 * @param {{segments: object[]}} input
 * @param {string} shape
 * @returns {{kind: "track"|"lanes"|"tree", tracks: object[][]}}
 */
export function miniViz({ segments = [] } = {}, shape = "") {
  if (!segments.length) return { kind: "track", tracks: [] };

  if (shape === "dag") {
    const byLayer = new Map();
    for (const segment of segments) {
      if (!byLayer.has(segment.layer)) byLayer.set(segment.layer, []);
      byLayer.get(segment.layer).push(segment);
    }
    return {
      kind: "tree",
      tracks: [...byLayer.keys()]
        .sort((a, b) => a - b)
        .map((layer) => byLayer.get(layer)),
    };
  }

  if (shape === "lanes") {
    // Independent runs, grouped over the edges core sent.
    const byId = new Map(segments.map((segment) => [segment.id, segment]));
    const neighbours = new Map(segments.map((segment) => [segment.id, []]));
    for (const segment of segments) {
      for (const parent of segment.after) {
        if (!byId.has(parent)) continue;
        neighbours.get(segment.id).push(parent);
        neighbours.get(parent).push(segment.id);
      }
    }
    const seen = new Set();
    const tracks = [];
    for (const segment of segments) {
      if (seen.has(segment.id)) continue;
      const group = [];
      const stack = [segment.id];
      seen.add(segment.id);
      while (stack.length) {
        const id = stack.pop();
        group.push(byId.get(id));
        for (const next of neighbours.get(id)) {
          if (seen.has(next)) continue;
          seen.add(next);
          stack.push(next);
        }
      }
      tracks.push(group);
    }
    return { kind: "lanes", tracks };
  }

  return { kind: "track", tracks: [segments] };
}

/**
 * One tile.
 *
 * @param {object} item a live initiatives projection row
 * @param {{ now?: number, href?: (ref: string) => string, excerptLimit?: number }} [options]
 */
export function initiativeTileModel(item, options = {}) {
  const { href = () => "", excerptLimit = 120 } = options;
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
    viz: miniViz(bars, shape),
    next,
    needs,
    /** ISO instant for the age badge; the badge owns the wording. */
    movedAt,
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
