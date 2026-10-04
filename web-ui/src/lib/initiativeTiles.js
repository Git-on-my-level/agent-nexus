/**
 * The Overview initiative tiles.
 *
 * A tile answers "what is the state and progress of this initiative" in a
 * glance, from the live initiatives projection alone. That projection is the
 * constraint worth knowing about: it carries `plan_state` — step ids with their
 * statuses, progress, critical path, next step ids, shape, health — but **not**
 * the step graph or the step titles. So a tile can show how much is done, what
 * shape the plan is and which steps are next, and it cannot draw the real tree
 * or print a step's title. Both of those are one click away on the initiative
 * page, which is the right place for them and the only place that can fetch
 * them without an N+1 across the grid.
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
 * Segments for the tile's mini-viz: one per step, in plan order, carrying the
 * status that colours it and whether it sits on the critical path.
 *
 * Capped, because a tile is a glance and a 200-step plan would render 200
 * hairlines. The overflow is reported so the caption can say so.
 */
export function planSegments(planState, limit = 24) {
  const steps = Array.isArray(planState?.steps) ? planState.steps : [];
  const critical = new Set(
    Array.isArray(planState?.critical_path) ? planState.critical_path : [],
  );
  const shown = steps.slice(0, limit);
  return {
    segments: shown.map((step) => ({
      id: asText(step?.id),
      status: asText(step?.status) || "not_started",
      onCriticalPath: critical.has(asText(step?.id)),
    })),
    overflow: Math.max(0, steps.length - shown.length),
  };
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

  const health = asText(item?.health);
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

  const shape = asText(planState?.shape);

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
    progress,
    shape,
    shapeLabel: SHAPE_LABELS[shape] ?? "",
    hasPlan: Boolean(planState),
    ...planSegments(planState),
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
