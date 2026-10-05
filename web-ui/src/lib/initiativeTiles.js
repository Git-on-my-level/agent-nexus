/**
 * The Overview initiative tiles.
 *
 * A tile answers "what is the state and progress of this initiative" in a
 * glance, from `/overview` alone — no per-tile fetch.
 *
 * Everything shown is computed by core and read here as given: `health` is
 * `{status, reason}`, `plan_state` carries progress, the critical path and the
 * next step ids, and `geometry` carries the bounded graph a mini-viz is drawn
 * from — `{shape, nodes: [{id, status, layer, after}], total_nodes,
 * collapsed_nodes}`, capped at 24 nodes with the remainder counted.
 *
 * The one thing the projection does not carry is step *titles*, so "Next: …"
 * reads the step id. Ids are lowercase slugs by contract, which makes that a
 * fair approximation; the titles are one click away on the initiative page.
 */

import { formatMovedAgo } from "./refResolve.js";

/** Health to the badge tones `SignalBadge` already defines. */
const HEALTH = Object.freeze({
  on_track: { label: "On track", tone: "ok" },
  stalled: { label: "Stalled", tone: "warn" },
  blocked: { label: "Blocked", tone: "danger" },
});

const SHAPE_LABELS = Object.freeze({
  chain: "Timeline",
  dag: "Tech tree",
  lanes: "Lanes",
});

const asText = (value) => String(value ?? "").trim();

/**
 * A step id read as prose. Step ids are lowercase slugs by contract
 * (`^[a-z0-9]+(-[a-z0-9]+)*$`), and the projection does not carry titles, so
 * this is the closest a tile can get to naming the next step without fetching
 * each plan separately.
 */
export function humanizeStepId(id) {
  const text = asText(id);
  if (!text) return "";
  const words = text.replaceAll("-", " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
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
 * @param {{ now?: number, href?: (ref: string) => string }} [options]
 */
export function initiativeTileModel(item, options = {}) {
  const { now = Date.now(), href = () => "" } = options;
  const ref = asText(item?.ref);
  const planState = item?.plan_state ?? null;
  const geometry = item?.geometry ?? null;

  // `health` is `{status, reason}`; core also sends a reason for a planless
  // initiative, derived from its native phase.
  const health = asText(item?.health?.status ?? item?.health);
  const badge = HEALTH[health] ?? null;

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

  const nextSteps = Array.isArray(planState?.next_steps)
    ? planState.next_steps.map(asText).filter(Boolean)
    : [];

  const movedAt =
    asText(planState?.last_movement_at) || asText(item?.updated_at);

  const shape = asText(geometry?.shape ?? planState?.shape);

  return {
    ref,
    title: asText(item?.title) || ref,
    href: href(ref),
    status: asText(item?.summary),
    phase: asText(item?.phase).replaceAll("_", " "),
    priority: asText(item?.priority),
    health,
    healthLabel: badge?.label ?? "",
    healthTone: badge?.tone ?? "neutral",
    healthReason: asText(item?.health?.reason),
    progress,
    shape,
    shapeLabel: SHAPE_LABELS[shape] ?? "",
    hasPlan: Boolean(planState),
    ...(() => {
      const bars = planSegments(planState, geometry);
      return {
        ...bars,
        viz: miniViz(bars, asText(geometry?.shape ?? planState?.shape)),
      };
    })(),
    // "Next: …" from the step id, since the projection has no titles.
    next: nextSteps.length ? humanizeStepId(nextSteps[0]) : "",
    extraNext: Math.max(0, nextSteps.length - 1),
    needs,
    movedLabel: formatMovedAgo(movedAt, now),
  };
}

/**
 * Tiles for the grid, in the order the projection sent them.
 *
 * @param {object[]} items
 * @param {{ now?: number, href?: (ref: string) => string }} [options]
 */
export function initiativeTiles(items = [], options = {}) {
  return (Array.isArray(items) ? items : [])
    .filter((item) => asText(item?.ref))
    .map((item) => initiativeTileModel(item, options));
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
