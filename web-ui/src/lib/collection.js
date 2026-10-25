/**
 * Collections: one row shape for every set of things that have a state,
 * progress, a time commitment and a next actor: loops and any
 * `live-collection` report panel. (Watch's initiatives use
 * `InitiativeRollupList`.) `CollectionTable` draws it; adapters here
 * build it from brief.v1 or from a panel's rendered data.
 *
 * Nothing here decides state. Adapters copy what core computed and say which
 * brief field each piece came from (`field`), so the table can tag it for the
 * I1 lint. Helpers are pure and take `now` so tests are deterministic.
 */

import {
  formatMetricValue,
  formatSpan,
  groupLoops,
  isLoop,
  plainInline,
} from "$lib/brief.js";
import { formatAgo } from "$lib/time/format.js";
import { withUnit } from "$lib/loops.js";

/** Five tones every collection shares; types only label them. */
export const TONES = ["attention", "warning", "quiet", "neutral", "good"];

const DAY_MS = 86_400_000;
export const ACTIVITY_DAYS = 14;

function text(value) {
  return String(value ?? "").trim();
}

/** Events per UTC day for the last `days` days, oldest first. */
export function activityByDay(
  timestamps,
  now = Date.now(),
  days = ACTIVITY_DAYS,
) {
  const counts = Array.from({ length: days }, () => 0);
  const today = Math.floor(now / DAY_MS);
  for (const iso of timestamps ?? []) {
    const at = Date.parse(text(iso));
    if (!Number.isFinite(at)) continue;
    const index = days - 1 - (today - Math.floor(at / DAY_MS));
    if (index >= 0 && index < days) counts[index] += 1;
  }
  return counts;
}

/** A sparkline's polyline points in a `width` × `height` box. */
export function sparkPoints(values, width = 88, height = 22, pad = 2) {
  const nums = (values ?? []).filter((v) => Number.isFinite(v));
  if (nums.length < 2) return "";
  let lo = Math.min(...nums);
  let hi = Math.max(...nums);
  if (lo === hi) {
    lo -= 1;
    hi += 1;
  }
  return nums
    .map((v, i) => {
      const x = pad + (i / (nums.length - 1)) * (width - 2 * pad);
      const y = pad + (1 - (v - lo) / (hi - lo)) * (height - 2 * pad);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

// ---------------------------------------------------------------------------
// Loops (brief.v1 `initiatives.loop`)

export const LOOP_STATES = [
  { key: "waiting", label: "Waiting on you", tone: "attention" },
  { key: "failing", label: "Failing", tone: "attention" },
  { key: "silent", label: "Stopped reporting", tone: "quiet" },
  { key: "flat", label: "Not improving", tone: "neutral" },
  { key: "moving", label: "Healthy", tone: "good" },
  { key: "no_runs", label: "No runs yet", tone: "neutral" },
  { key: "done", label: "Done", tone: "quiet" },
];

/** groupLoops calls a loop with no receipts "moving"; say what it is. */
function loopState(group, row) {
  return group === "moving" && row.loop?.condition === "unknown"
    ? "no_runs"
    : group;
}

function loopProgress(row) {
  const goal = row.loop?.goal;
  if (goal?.points?.length) {
    const latest = goal.latest ?? goal.points[goal.points.length - 1].value;
    return {
      kind: "spark",
      values: goal.points.map((p) => p.value),
      label: withUnit(formatMetricValue(latest), goal.unit),
      // The author's words for what is measured, else the metric key.
      metric: goal.label || goal.metric,
      direction: goal.direction,
    };
  }
  const metric = row.series?.[0];
  if (metric) {
    const value = formatMetricValue(metric.last_value);
    return {
      kind: "label",
      label: value ? `${metric.name || metric.ref} ${value}` : "no data",
    };
  }
  if (row.loop) {
    const r = row.loop.runs7d;
    const total = r.acted + r.measured + r.no_op + r.infra_error;
    return {
      kind: "label",
      label: total ? `${total} runs this week` : "no runs this week",
    };
  }
  return null;
}

function loopTime(row, now) {
  const loop = row.loop;
  if (loop?.lastRunAt) {
    const every = loop.everySeconds
      ? ` · every ${formatSpan(loop.everySeconds)}`
      : "";
    return {
      text: `${formatAgo(loop.lastRunAt, now)}${every}`,
      title: "",
      tone: loop.condition === "overdue" ? "warning" : "neutral",
      field: "initiatives.loop",
    };
  }
  if (loop?.everySeconds)
    return {
      text: `every ${formatSpan(loop.everySeconds)}`,
      title: "",
      tone: "neutral",
      field: "initiatives.loop",
    };
  return null;
}

export function collectionFromLoops(
  initiatives,
  { now = Date.now(), href, decideHref } = {},
) {
  const groups = groupLoops((initiatives ?? []).filter(isLoop), now);
  const rows = groups.flatMap((group) =>
    group.rows.map((row) => ({
      ref: row.ref,
      title: row.title,
      href: href ? href(row) : "",
      subtitle: row.promise?.what
        ? { text: plainInline(row.promise.what), field: "initiatives.promise" }
        : row.loop?.reason
          ? { text: row.loop.reason, field: "initiatives.loop" }
          : null,
      owner: null,
      state: loopState(group.group, row),
      stateField:
        group.group === "waiting"
          ? "initiatives.waiting_on_you_count"
          : "initiatives.loop",
      progress: loopProgress(row),
      time: loopTime(row, now),
      activity: row.loop?.recentRuns?.length
        ? activityByDay(
            row.loop.recentRuns.map((run) => run.at),
            now,
          )
        : null,
      next:
        row.waitingOnYouCount > 0
          ? {
              label: "You",
              isViewer: true,
              href: decideHref ? decideHref(row) : "",
              field: "initiatives.waiting_on_you_count",
            }
          : null,
      changed: false,
      source: { kind: "loop", initiative: row },
    })),
  );
  return groupRows(LOOP_STATES, rows);
}

// ---------------------------------------------------------------------------
// `live-collection` report panels (rendered by core)

function normalizeProgress(raw) {
  if (!raw || typeof raw !== "object") return null;
  if (raw.kind === "segments" && Array.isArray(raw.segments))
    return {
      kind: "segments",
      segments: raw.segments.map((s) => ({
        label: text(s?.label),
        status: ["done", "current", "blocked", "todo"].includes(s?.status)
          ? s.status
          : "todo",
      })),
      label: text(raw.label),
    };
  if (raw.kind === "spark" && Array.isArray(raw.points))
    return {
      kind: "spark",
      values: raw.points.map((p) => Number(p?.v)).filter(Number.isFinite),
      label: text(raw.label),
      direction:
        raw.direction === "down"
          ? "down"
          : raw.direction === "up"
            ? "up"
            : null,
    };
  if (text(raw.label)) return { kind: "label", label: text(raw.label) };
  return null;
}

/** Normalize core's `live-collection` data; unknown states stay visible as neutral. */
export function normalizeCollection(raw, { href } = {}) {
  const states = (Array.isArray(raw?.states) ? raw.states : [])
    .map((s) => ({
      key: text(s?.key),
      label: text(s?.label) || text(s?.key),
      tone: TONES.includes(s?.tone) ? s.tone : "neutral",
    }))
    .filter((s) => s.key);
  const known = new Set(states.map((s) => s.key));
  const groups = (Array.isArray(raw?.groups) ? raw.groups : []).map((g) => ({
    key: text(g?.key) || "all",
    label: text(g?.label),
    rows: (Array.isArray(g?.rows) ? g.rows : [])
      .filter((r) => text(r?.ref))
      .map((r) => {
        const state = text(r.state);
        if (state && !known.has(state)) {
          known.add(state);
          states.push({
            key: state,
            label: state.replace(/_/g, " "),
            tone: "neutral",
          });
        }
        return {
          ref: text(r.ref),
          title: text(r.title) || text(r.ref),
          href: href ? href(r) : "",
          subtitle: text(r.subtitle) ? { text: text(r.subtitle) } : null,
          owner: r.owner?.label
            ? { label: text(r.owner.label), ref: text(r.owner.ref) }
            : null,
          state,
          stateField: "",
          progress: normalizeProgress(r.progress),
          time: r.time?.label
            ? {
                text: text(r.time.label),
                title: "",
                tone: r.time.tone === "warning" ? "warning" : "neutral",
              }
            : null,
          activity:
            Array.isArray(r.activity) && r.activity.length
              ? r.activity.map((n) => Math.max(0, Number(n) || 0))
              : null,
          next: r.next_actor?.label
            ? {
                label: text(r.next_actor.label),
                isViewer: Boolean(r.next_actor.is_viewer),
                href: "",
              }
            : null,
          changed: Boolean(r.changed_since_last_visit),
          source: { kind: "collection" },
        };
      }),
  }));
  const counts = {};
  for (const g of groups)
    for (const r of g.rows) counts[r.state] = (counts[r.state] ?? 0) + 1;
  return { states, groups, counts, hasMore: Boolean(raw?.has_more) };
}

// ---------------------------------------------------------------------------

function groupRows(states, rows) {
  const counts = {};
  for (const row of rows) counts[row.state] = (counts[row.state] ?? 0) + 1;
  return {
    states,
    groups: [{ key: "all", label: "", rows }],
    counts,
    hasMore: false,
  };
}

/**
 * The same collection without brief field tags, for report panels: their
 * data is a panel render, not brief.v1, so they must not claim brief fields.
 */
export function untagged(collection) {
  const strip = (part) => (part ? { ...part, field: "" } : part);
  return {
    ...collection,
    groups: collection.groups.map((g) => ({
      ...g,
      rows: g.rows.map((r) => ({
        ...r,
        stateField: "",
        subtitle: r.subtitle?.parts
          ? { parts: r.subtitle.parts.map(strip) }
          : strip(r.subtitle),
        time: strip(r.time),
        next: strip(r.next),
        progress: r.progress ? { ...r.progress, untagged: true } : null,
      })),
    })),
  };
}

/**
 * When most rows share one quiet state (every loop stopped reporting), say
 * it once instead of on every row: `{ state, count, total, rows }` or null,
 * where `rows` are the quiet ones.
 */
export function dominantQuietState(collection, share = 0.8) {
  const quiet = new Set(
    collection.states.filter((s) => s.tone === "quiet").map((s) => s.key),
  );
  const rows = collection.groups.flatMap((g) => g.rows);
  if (rows.length < 3) return null;
  for (const key of quiet) {
    if (key === "done") continue;
    const matching = rows.filter((r) => r.state === key);
    if (matching.length / rows.length >= share)
      return {
        state: key,
        count: matching.length,
        total: rows.length,
        rows: matching,
      };
  }
  return null;
}

/** Rows filtered to one state (or all), in state order. */
export function filterRows(collection, stateKey) {
  const order = new Map(collection.states.map((s, i) => [s.key, i]));
  return collection.groups.map((g) => ({
    ...g,
    rows: g.rows
      .filter((r) => !stateKey || r.state === stateKey)
      .sort((a, b) => (order.get(a.state) ?? 99) - (order.get(b.state) ?? 99)),
  }));
}

/** Rows bucketed by state for the board layout. */
export function boardColumns(collection) {
  const rows = collection.groups.flatMap((g) => g.rows);
  return collection.states
    .map((s) => ({ ...s, rows: rows.filter((r) => r.state === s.key) }))
    .filter((c) => c.rows.length);
}

/** How many rows need the reader (attention tone), for tab badges. */
export function attentionCount(collection) {
  const hot = new Set(
    collection.states.filter((s) => s.tone === "attention").map((s) => s.key),
  );
  return collection.groups.reduce(
    (n, g) => n + g.rows.filter((r) => hot.has(r.state)).length,
    0,
  );
}

/** Which optional columns any row fills, so empty columns are not drawn. */
export function columnsFor(collection) {
  const rows = collection.groups.flatMap((g) => g.rows);
  return {
    owner: rows.some((r) => r.owner),
    progress: rows.some((r) => r.progress),
    time: rows.some((r) => r.time),
    activity: rows.some((r) => r.activity),
    next: rows.some((r) => r.next),
  };
}
