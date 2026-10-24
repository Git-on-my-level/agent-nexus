/**
 * Loops: an initiative card with a cadence, a goal, or run receipts. Core
 * computes the loop's condition from its receipts (`initiatives.loop` on
 * brief.v1, and the `live-loops` / `live-loop` report panels); this module
 * only normalizes that object and does display arithmetic. It never decides
 * a condition itself.
 */

export const LOOP_CONDITIONS = [
  "failing",
  "overdue",
  "stalled",
  "healthy",
  "unknown",
];

/** The words each computed condition is shown as, and its tone. */
export const LOOP_CONDITION_TEXT = {
  failing: { text: "Failing", tone: "danger" },
  overdue: { text: "Overdue", tone: "warn" },
  stalled: { text: "Stalled", tone: "warn" },
  healthy: { text: "Healthy", tone: "ok" },
  unknown: { text: "No runs yet", tone: "neutral" },
};

export const RUN_OUTCOMES = ["acted", "measured", "no_op", "infra_error"];

export const RUN_OUTCOME_TEXT = {
  acted: "acted",
  measured: "measured",
  no_op: "no-op",
  infra_error: "error",
};

const DECISION_TEXT = {
  kept: "kept",
  reverted: "reverted",
  rejected: "rejected",
};

function str(value) {
  return typeof value === "string" ? value : "";
}

function num(value) {
  const n = typeof value === "number" ? value : Number(value);
  return value === null ||
    value === undefined ||
    value === "" ||
    !Number.isFinite(n)
    ? null
    : n;
}

function count(value) {
  return Math.max(0, Math.floor(num(value) ?? 0));
}

function normalizeRun(raw) {
  if (!raw || typeof raw !== "object") return null;
  const at = str(raw.at);
  const outcome = RUN_OUTCOMES.includes(raw.outcome) ? raw.outcome : null;
  if (!at || !outcome) return null;
  const decision = DECISION_TEXT[raw.decision] ? raw.decision : null;
  return { at, outcome, decision };
}

function normalizeGoal(raw) {
  if (!raw || typeof raw !== "object" || !str(raw.metric)) return null;
  const points = (Array.isArray(raw.points) ? raw.points : [])
    .map((point) => {
      const value = num(point?.value);
      const at = str(point?.at);
      if (value === null || !at) return null;
      return {
        at,
        value,
        decision: DECISION_TEXT[point.decision] ? point.decision : null,
      };
    })
    .filter(Boolean)
    .sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
  return {
    metric: raw.metric,
    direction: raw.direction === "down" ? "down" : "up",
    target: num(raw.target),
    baseline: num(raw.baseline),
    unit: str(raw.unit),
    latest: num(raw.latest),
    best: num(raw.best),
    points,
  };
}

/** Normalize a computed `loop` object; null when absent or unreadable. */
export function normalizeLoop(raw) {
  if (!raw || typeof raw !== "object") return null;
  const condition = LOOP_CONDITIONS.includes(raw.health)
    ? raw.health
    : "unknown";
  const runs7d = raw.runs_7d ?? {};
  return {
    condition,
    reason: str(raw.health_reason),
    everySeconds: num(raw.every_seconds),
    budgetSeconds: num(raw.progress_budget_seconds),
    stopRule: str(raw.stop_rule),
    lastRunAt: str(raw.last_run_at),
    lastEffectAt: str(raw.last_effect_at),
    nextRunDueAt: str(raw.next_run_due_at),
    recentRuns: (Array.isArray(raw.recent_runs) ? raw.recent_runs : [])
      .map(normalizeRun)
      .filter(Boolean)
      .sort((a, b) => Date.parse(a.at) - Date.parse(b.at)),
    // Runs that changed something: acted, or kept a candidate. Core counts
    // this; older cores only send the outcome mix, so fall back to acted.
    effects7d:
      raw.effects_7d === undefined || raw.effects_7d === null
        ? count(runs7d.acted)
        : count(raw.effects_7d),
    runs7d: {
      acted: count(runs7d.acted),
      measured: count(runs7d.measured),
      no_op: count(runs7d.no_op),
      infra_error: count(runs7d.infra_error),
    },
    goal: normalizeGoal(raw.goal),
  };
}

/** One run receipt from the `live-loop` panel, newest first in the panel. */
export function normalizeRunReceipt(raw) {
  const run = normalizeRun(raw);
  if (!run) return null;
  const experiment =
    raw.experiment && typeof raw.experiment === "object"
      ? {
          candidate: str(raw.experiment.candidate),
          decision: DECISION_TEXT[raw.experiment.decision]
            ? raw.experiment.decision
            : null,
          before: num(raw.experiment.score_before),
          after: num(raw.experiment.score_after),
        }
      : null;
  const metrics = Object.entries(raw.metrics ?? {})
    .map(([name, value]) => ({ name, value: num(value) }))
    .filter((metric) => metric.value !== null);
  return {
    ...run,
    decision: run.decision ?? experiment?.decision ?? null,
    summary: str(raw.summary),
    runId: str(raw.run_id),
    eventId: str(raw.event_id),
    metrics,
    experiment,
    effectRefs: (Array.isArray(raw.effect_refs) ? raw.effect_refs : []).filter(
      (ref) => typeof ref === "string" && ref,
    ),
  };
}

/**
 * Consecutive runs that say the same thing (same outcome, decision and
 * summary, no candidate or effects) fold into one row with a count and a
 * date span, so twelve identical no-ops read as "12× no-op".
 */
export function collapseRuns(runs) {
  const out = [];
  const plain = (run) =>
    !run.experiment?.candidate && !(run.effectRefs?.length > 0);
  for (const run of runs ?? []) {
    const prev = out[out.length - 1];
    if (
      prev &&
      plain(run) &&
      plain(prev) &&
      prev.outcome === run.outcome &&
      prev.decision === run.decision &&
      prev.summary === run.summary
    ) {
      prev.count += 1;
      prev.oldestAt = run.at;
      continue;
    }
    out.push({ ...run, count: 1, oldestAt: run.at });
  }
  return out;
}

/** A candidate id short enough to read: git-style 7 characters for hashes. */
export function shortCandidate(candidate) {
  const value = str(candidate);
  return /^[0-9a-f]{12,}$/i.test(value) ? value.slice(0, 7) : value;
}

/**
 * Brief-shaped rows for loop objects that came without an initiative (the
 * `live-loops` panel), so the panel can use the shared collection rows.
 */
export function loopPanelRows(rows) {
  return (rows ?? [])
    .filter((row) => row?.loop)
    .map((row) => ({
      ref: row.ref,
      id: String(row.ref).replace(/^card:/, ""),
      title: row.title,
      state: "moving",
      waitingOnYouCount: 0,
      series: [],
      metricFlat: null,
      promise: null,
      lastEvidenceAt: row.loop.lastRunAt,
      loop: row.loop,
    }));
}

/** Attention order: failing, overdue, stalled, healthy, then no runs. */
export function compareLoops(a, b) {
  const rank = (row) =>
    LOOP_CONDITIONS.indexOf(row.loop?.condition ?? "unknown");
  const byRank = rank(a) - rank(b);
  if (byRank) return byRank;
  const effect = (row) => Date.parse(row.loop?.lastEffectAt || "") || 0;
  return effect(b) - effect(a);
}

/**
 * Ecosystem totals over a set of loops, when the server did not send its own
 * `stats` (Watch derives them from the brief's loops).
 */
export function loopTotals(loops) {
  const totals = {
    loops: 0,
    effects7d: 0,
    runs7d: { acted: 0, measured: 0, no_op: 0, infra_error: 0 },
    needsAttention: 0,
  };
  for (const loop of loops) {
    if (!loop) continue;
    totals.loops += 1;
    for (const outcome of RUN_OUTCOMES)
      totals.runs7d[outcome] += loop.runs7d[outcome];
    totals.effects7d += loop.effects7d;
    if (["failing", "overdue", "stalled"].includes(loop.condition))
      totals.needsAttention += 1;
  }
  return totals;
}

export function normalizeLoopStats(raw) {
  if (!raw || typeof raw !== "object") return null;
  const runs = raw.runs_7d ?? {};
  return {
    loops: count(raw.loops),
    effects7d: count(raw.effects_7d),
    effectsPrev7d:
      raw.effects_prev_7d === undefined ? null : count(raw.effects_prev_7d),
    runs7d: {
      acted: count(runs.acted),
      measured: count(runs.measured),
      no_op: count(runs.no_op),
      infra_error: count(runs.infra_error),
    },
    improved7d: raw.improved_7d === undefined ? null : count(raw.improved_7d),
    needsAttention: count(raw.needs_attention),
  };
}

/** A formatted goal value (from `formatMetricValue`) with its unit. */
export function withUnit(text, unit = "") {
  if (!text) return "";
  if (!unit) return text;
  return unit === "%" || unit === "pct" ? `${text}%` : `${text} ${unit}`;
}

/** How far the best value is from the target, signed so positive is "still to go". */
export function goalGap(goal) {
  if (!goal || goal.target === null || goal.best === null) return null;
  return goal.direction === "down"
    ? goal.best - goal.target
    : goal.target - goal.best;
}

/** A point the loop adopted (or measured without a candidate). */
export function isIncumbent(point) {
  return point?.decision !== "rejected" && point?.decision !== "reverted";
}

/**
 * Chart geometry for a goal's points, in a `width` × `height` box with
 * `pad` inside. Returns the raw polyline, the best-so-far step line, the
 * target's y, and marker positions for kept/reverted runs.
 */
export function goalGeometry(
  goal,
  { width = 300, height = 64, pad = 4, window = null } = {},
) {
  const points = goal?.points ?? [];
  if (!points.length) return null;
  const values = points.map((point) => point.value);
  if (goal.target !== null) values.push(goal.target);
  let lo = Math.min(...values);
  let hi = Math.max(...values);
  if (lo === hi) {
    const spread = Math.abs(lo) * 0.05 || 1;
    lo -= spread;
    hi += spread;
  }
  const times = points.map((point) => Date.parse(point.at));
  // With a window, time runs across it (a burst of runs stays a burst, and a
  // silence shows as empty space). Without one, first point to last.
  const useWindow =
    window && Number.isFinite(window.start) && window.end > window.start;
  const t0 = useWindow ? window.start : times[0];
  const t1 = useWindow
    ? Math.max(window.end, times[times.length - 1])
    : times[times.length - 1];
  const clampX = (value) => Math.min(width - pad, Math.max(pad, value));
  const x = (index) =>
    t1 === t0
      ? points.length === 1
        ? width / 2
        : pad + (index / (points.length - 1)) * (width - 2 * pad)
      : clampX(pad + ((times[index] - t0) / (t1 - t0)) * (width - 2 * pad));
  const y = (value) =>
    pad + (1 - (value - lo) / (hi - lo)) * (height - 2 * pad);
  const better = (a, b) => (goal.direction === "down" ? a < b : a > b);
  const coords = points.map((point, index) => [x(index), y(point.value)]);
  // Best so far follows the incumbent: a rejected or reverted candidate was
  // measured but never adopted, so it is marked but does not raise the line.
  let best = null;
  const step = [];
  points.forEach((point, index) => {
    if (isIncumbent(point) && (best === null || better(point.value, best))) {
      if (best !== null) step.push([x(index), y(best)]);
      best = point.value;
    }
    if (best !== null) step.push([x(index), y(best)]);
  });
  const fmt = (pairs) =>
    pairs.map(([px, py]) => `${px.toFixed(1)},${py.toFixed(1)}`).join(" ");
  return {
    raw: fmt(coords),
    best: fmt(step),
    target: goal.target === null ? null : y(goal.target),
    markers: points
      .map((point, index) =>
        point.decision
          ? {
              x: coords[index][0],
              y: coords[index][1],
              decision: point.decision,
              at: point.at,
              value: point.value,
            }
          : null,
      )
      .filter(Boolean),
    last: { x: coords[coords.length - 1][0], y: coords[coords.length - 1][1] },
    // No data since the last point for over a tenth of the window: draw the
    // silence as a dashed line to the window's end.
    silence:
      useWindow &&
      t1 - times[times.length - 1] > (t1 - t0) / 10 &&
      best !== null
        ? { x1: coords[coords.length - 1][0], x2: width - pad, y: y(best) }
        : null,
    lo,
    hi,
  };
}

/**
 * Where an effect ref may link: an http(s) URL, or the in-app route the
 * resolver gave for a typed ref. Anything else (`javascript:`, `data:`) is
 * shown as text, never as a link.
 */
export function safeEffectHref(href) {
  if (typeof href !== "string" || !href) return "";
  if (href.startsWith("/") && !href.startsWith("//")) return href;
  try {
    const url = new URL(href);
    return url.protocol === "https:" || url.protocol === "http:"
      ? url.href
      : "";
  } catch {
    return "";
  }
}

/** "every 1d · budget 7d" style cadence text. */
export function cadenceText(loop, formatSpan) {
  const parts = [];
  if (loop?.everySeconds) parts.push(`every ${formatSpan(loop.everySeconds)}`);
  if (loop?.budgetSeconds)
    parts.push(`effect budget ${formatSpan(loop.budgetSeconds)}`);
  return parts.join(" · ");
}

export function runText(run) {
  const base = RUN_OUTCOME_TEXT[run.outcome] ?? run.outcome;
  return run.decision ? `${base}, ${DECISION_TEXT[run.decision]}` : base;
}
