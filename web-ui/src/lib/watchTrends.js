/**
 * Watch → Trends: presentation for `watch_trends.v1`.
 *
 * Core computes every number; this module only words them. A takeaway is one
 * plain sentence built from a panel's own value and previous value, and says
 * "not enough history" rather than guessing when either is unknown.
 */

import { formatUtcDay } from "$lib/time/format.js";

export const TREND_RANGES = Object.freeze([7, 30, 90]);
export const TREND_GROUPS = Object.freeze([
  { key: "decisions", label: "Decisions" },
  { key: "initiatives", label: "Initiatives" },
  { key: "loops", label: "Loops" },
]);

/** `?range=7d|30d|90d`, defaulting to 30 days. */
export function trendRange(params) {
  const days = Number.parseInt(String(params?.get?.("range") ?? ""), 10);
  return TREND_RANGES.includes(days) ? days : 30;
}

const isNumber = (value) => typeof value === "number" && Number.isFinite(value);

/** "previous 30 days", "previous week". */
export function previousPeriod(days) {
  return days === 7 ? "previous week" : `previous ${days} days`;
}

function round(value, places = 0) {
  const factor = 10 ** places;
  return Math.round(value * factor) / factor;
}

/** A duration in plain words: "45 min", "3.5 h", "2.1 days". */
export function formatDuration(seconds) {
  if (!isNumber(seconds)) return "—";
  if (seconds < 90) return `${Math.max(0, Math.round(seconds))} s`;
  const minutes = seconds / 60;
  if (minutes < 90) return `${Math.round(minutes)} min`;
  const hours = minutes / 60;
  if (hours < 36) return `${round(hours, hours < 10 ? 1 : 0)} h`;
  const days = hours / 24;
  return `${round(days, days < 10 ? 1 : 0)} days`;
}

export function formatTrendValue(unit, value) {
  if (!isNumber(value)) return "—";
  if (unit === "seconds") return formatDuration(value);
  if (unit === "percent") return `${Math.round(value)}%`;
  return new Intl.NumberFormat("en-US").format(round(value, 1));
}

/**
 * The change from the previous period, and whether it is an improvement.
 * Percentages move in points; counts and durations as a ratio.
 *
 * @returns {null | { label: string, tone: "good" | "bad" | "flat" }}
 */
export function trendDelta(panel) {
  const { value, previous, unit, direction } = panel ?? {};
  if (!isNumber(value) || !isNumber(previous)) return null;
  const diff = value - previous;
  let label;
  let flat;
  if (unit === "percent") {
    const points = Math.round(diff);
    flat = points === 0;
    label = flat
      ? "No change"
      : `${points > 0 ? "▲" : "▼"} ${Math.abs(points)} pts`;
  } else if (previous === 0) {
    flat = value === 0;
    label = flat ? "No change" : "▲ new";
  } else {
    const ratio = value / previous;
    flat = Math.abs(ratio - 1) < 0.05;
    label = flat
      ? "No change"
      : ratio >= 2
        ? `▲ ${round(ratio, 1)}×`
        : `${ratio > 1 ? "▲" : "▼"} ${Math.round(Math.abs(ratio - 1) * 100)}%`;
  }
  if (flat || direction === "neutral") return { label, tone: "flat" };
  const better = direction === "down_good" ? diff < 0 : diff > 0;
  return { label, tone: better ? "good" : "bad" };
}

/** How the value changed, as words: "up 2.3×", "down 40%", "about the same". */
function change(value, previous) {
  if (!isNumber(value) || !isNumber(previous)) return "";
  if (previous === 0) return value === 0 ? "about the same" : "up from none";
  // A fall to zero has no factor: dividing by it printed "down Infinity×".
  if (value === 0) return "down to none";
  const ratio = value / previous;
  if (Math.abs(ratio - 1) < 0.1) return "about the same";
  if (ratio >= 2) return `up ${round(ratio, 1)}×`;
  if (ratio <= 0.5) return `down ${round(1 / ratio, 1)}×`;
  return `${ratio > 1 ? "up" : "down"} ${Math.round(Math.abs(ratio - 1) * 100)}%`;
}

function plural(n, word, many = `${word}s`) {
  return `${formatTrendValue("count", n)} ${n === 1 ? word : many}`;
}

/**
 * One plain sentence about what changed.
 *
 * @param {object} panel a `watch_trends.v1` panel
 * @param {number} days the range in days
 */
export function trendTakeaway(panel, days) {
  if (!panel) return "";
  const { id, value, previous, unit, truncated, rows = [] } = panel;
  const before = previousPeriod(days);
  const since = change(value, previous);
  const vs = since ? ` vs the ${before}` : "";

  if (id === "ask_sources" || id === "agent_runs") {
    const top = rows[0];
    // Core leaves a ranking empty when the period was not read in full; that
    // is not the same as no activity.
    if (!top && truncated)
      return "Busy period: not every record was read, so there is no ranking.";
    if (!top)
      return id === "ask_sources"
        ? "No asks raised in this period."
        : "No runs recorded in this period.";
    const name = top.principal?.display || top.principal?.id || "Someone";
    const what =
      id === "ask_sources"
        ? plural(top.value, "ask")
        : plural(top.value, "run");
    const trend =
      isNumber(top.previous) && top.previous !== top.value
        ? `, ${top.previous < top.value ? "up" : "down"} from ${formatTrendValue("count", top.previous)}`
        : "";
    return id === "ask_sources"
      ? `${name} needed the most help: ${what}${trend}.`
      : `${name} was the most active: ${what}${trend}.`;
  }

  if (!isNumber(value)) {
    if (truncated)
      return "Busy period: only the most recent records were read, so it can't be summed.";
    return id === "answer_time"
      ? "No asks answered in this period yet."
      : "Not enough history yet.";
  }

  switch (id) {
    case "asks_flow": {
      const raised = panel.raised;
      if (isNumber(raised) && raised > value)
        return `Asks are piling up: ${plural(raised, "raised", "raised")}, ${formatTrendValue("count", value)} answered.`;
      if (isNumber(raised))
        return `Answers are keeping up: ${formatTrendValue("count", value)} answered, ${formatTrendValue("count", raised)} raised.`;
      break;
    }
    case "answer_time":
      if (!since || since === "about the same")
        return `Typical wait for an answer: ${formatDuration(value)}${since ? `, about the same as the ${before}` : ""}.`;
      return `Answer time ${since}${vs}, now ${formatDuration(value)}.`;
    case "ask_backlog":
      if (!isNumber(previous))
        return `${plural(value, "ask")} waiting for an answer.`;
      if (value === previous)
        return `Open asks steady at ${formatTrendValue("count", value)}.`;
      return `Open asks ${value > previous ? "up" : "down"} from ${formatTrendValue("count", previous)} to ${formatTrendValue("count", value)}.`;
    case "promises": {
      const due = isNumber(panel.due)
        ? ` of ${plural(panel.due, "promise")} due`
        : "";
      const was = isNumber(previous)
        ? `, ${Math.round(previous)}% in the ${before}`
        : "";
      return `${formatTrendValue("percent", value)}${due} kept${was}.`;
    }
    case "past_promise": {
      const count =
        isNumber(panel.past) && isNumber(panel.total)
          ? `${formatTrendValue("count", panel.past)} of ${formatTrendValue("count", panel.total)} initiatives are past their promise`
          : `${formatTrendValue("percent", value)} of initiatives are past their promise`;
      if (!isNumber(previous)) return `${count}.`;
      const points = Math.round(value - previous);
      return points === 0
        ? `${count}, unchanged.`
        : `${count}, ${points > 0 ? "up" : "down"} ${Math.abs(points)} pts.`;
    }
    case "work_flow": {
      const started = panel.started;
      if (isNumber(started))
        return started > value
          ? `Starting more than finishing: ${formatTrendValue("count", started)} started, ${formatTrendValue("count", value)} finished.`
          : `Finishing as fast as starting: ${formatTrendValue("count", value)} finished, ${formatTrendValue("count", started)} started.`;
      break;
    }
    case "loop_runs": {
      const failed =
        isNumber(panel.failed) && panel.failed > 0
          ? `; ${plural(panel.failed, "run")} failed to run`
          : "";
      return since
        ? `Runs ${since}${vs}${failed}.`
        : `${plural(value, "run")} recorded${failed}.`;
    }
    case "loop_health":
      if (!isNumber(previous))
        return `${formatTrendValue("percent", value)} of loops are reporting on time.`;
      return `${formatTrendValue("percent", value)} of loops reporting on time, ${Math.round(value) === Math.round(previous) ? "unchanged" : `${value > previous ? "up" : "down"} from ${Math.round(previous)}%`}.`;
  }
  const shown = formatTrendValue(unit, value);
  return since ? `${shown}, ${since}${vs}.` : `${shown} in this period.`;
}

/**
 * SVG path data for a sparkline in a `width`×`height` box. Null points break
 * the line: unknown history is a gap, never a dip to zero.
 */
export function sparklinePath(points, { width = 100, height = 32, max } = {}) {
  const known = points.filter(isNumber);
  if (!known.length) return "";
  const top = Math.max(isNumber(max) ? max : Math.max(...known), 1);
  const step = points.length > 1 ? width / (points.length - 1) : 0;
  let d = "";
  let pen = false;
  points.forEach((value, i) => {
    if (!isNumber(value)) {
      pen = false;
      return;
    }
    const x = round(points.length > 1 ? i * step : width / 2, 2);
    const y = round(height - (value / top) * (height - 2) - 1, 2);
    d += `${pen ? "L" : "M"}${x} ${y}`;
    pen = true;
  });
  return d;
}

/**
 * The scale every series in a panel shares. A single-series percentage is a
 * share and keeps the full 0–100 scale; several series are the counts behind
 * a share (kept and missed) and scale to their own largest value.
 */
export function panelMax(panel) {
  const series = panel?.series ?? [];
  if (panel?.unit === "percent" && series.length === 1) return 100;
  const values = series.flatMap((s) => (s.points ?? []).filter(isNumber));
  return values.length ? Math.max(...values) : 0;
}

/**
 * The words a screen reader hears for a panel's chart. Each series is
 * formatted in its own unit: a percentage panel can chart counts (promises
 * kept is a share of kept and missed counts), and "Kept 2%" would misstate
 * them. Older responses without a series unit fall back to the panel's.
 */
export function trendChartLabel(panel) {
  const series = (panel?.series ?? [])
    .map(
      (s) =>
        `${s.label} ${(s.points ?? [])
          .map((p) =>
            isNumber(p) ? formatTrendValue(s.unit ?? panel.unit, p) : "unknown",
          )
          .join(", ")}`,
    )
    .join("; ");
  return `${panel?.title ?? ""}: ${series}`;
}

// --- Health summary (Watch → Trends layout) ---

/** The series a count panel's value totals. */
const VALUE_SERIES = Object.freeze({
  asks_flow: "answered",
  work_flow: "finished",
  loop_runs: "runs",
  loop_failed: "failed",
});

const seriesPoints = (panel, key) =>
  (panel?.series ?? []).find((s) => s.key === key)?.points ?? [];

/** How many of the most recent buckets have a known value. */
export function knownTail(points = []) {
  let n = 0;
  for (let i = points.length - 1; i >= 0 && isNumber(points[i]); i--) n++;
  return n;
}

const span = (n, bucket) =>
  `${n} ${bucket === "week" ? (n === 1 ? "week" : "weeks") : n === 1 ? "day" : "days"}`;

/**
 * What a panel's headline shows. A count panel whose older history could not
 * be read shows the total over the buckets that were, and says over how long,
 * rather than a dash.
 *
 * @returns {{ text: string, delta: ReturnType<typeof trendDelta>, partial: string }}
 */
export function panelReading(panel, bucket = "day") {
  if (!panel) return { text: "—", delta: null, partial: "" };
  if (isNumber(panel.value))
    return {
      text: formatTrendValue(panel.unit, panel.value),
      delta: trendDelta(panel),
      partial: "",
    };
  const key = VALUE_SERIES[panel.id];
  const points = key ? seriesPoints(panel, key) : [];
  const known = knownTail(points);
  if (known > 0 && known < points.length) {
    const total = points.slice(-known).reduce((sum, v) => sum + v, 0);
    return {
      text: formatTrendValue("count", total),
      delta: null,
      partial: `last ${span(known, bucket)}`,
    };
  }
  return { text: "—", delta: null, partial: "" };
}

/** "Oct 8"; weekly buckets read "Week of Oct 8". */
export function bucketLabel(iso, bucket = "day") {
  const day = formatUtcDay(iso);
  return day && bucket === "week" ? `Week of ${day}` : day;
}

const byId = (panels) =>
  Object.fromEntries((panels ?? []).map((p) => [p.id, p]));

/**
 * One area's status pill: "warn" when it is getting worse, "ok" when it is
 * holding, "none" when there is not enough history to say.
 *
 * @returns {{ tone: "warn" | "ok" | "none", label: string }}
 */
export function areaStatus(group, panels) {
  const p = byId(panels);
  const none = { tone: "none", label: "Not enough history" };
  if (group === "decisions") {
    const backlog = p.ask_backlog;
    if (isNumber(backlog?.value) && isNumber(backlog?.previous)) {
      if (backlog.value > backlog.previous)
        return { tone: "warn", label: "Backlog growing" };
      return {
        tone: "ok",
        label:
          backlog.value < backlog.previous ? "Backlog shrinking" : "Steady",
      };
    }
    const flow = p.asks_flow;
    if (isNumber(flow?.raised) && isNumber(flow?.value))
      return flow.raised > flow.value
        ? { tone: "warn", label: "More asks than answers" }
        : { tone: "ok", label: "Keeping up" };
    return none;
  }
  if (group === "initiatives") {
    const past = p.past_promise;
    const kept = trendDelta(p.promises ?? {});
    if (isNumber(past?.value) && isNumber(past?.previous))
      return past.value > past.previous || kept?.tone === "bad"
        ? { tone: "warn", label: "Slipping" }
        : { tone: "ok", label: "Holding" };
    if (kept)
      return kept.tone === "bad"
        ? { tone: "warn", label: "Slipping" }
        : { tone: "ok", label: "Holding" };
    const work = p.work_flow;
    if (isNumber(work?.started) && isNumber(work?.value))
      return work.started > work.value
        ? { tone: "warn", label: "Starting more than finishing" }
        : { tone: "ok", label: "Holding" };
    return none;
  }
  if (group === "loops") {
    const health = p.loop_health;
    const runs = p.loop_runs;
    if (isNumber(health?.value) && health.value < 100)
      return { tone: "warn", label: `${Math.round(health.value)}% on time` };
    if (isNumber(runs?.failed) && runs.failed > 0)
      return { tone: "warn", label: `${plural(runs.failed, "failed run")}` };
    if (isNumber(health?.value) || isNumber(runs?.value))
      return { tone: "ok", label: "Healthy" };
    return none;
  }
  return none;
}

function joinClauses(clauses) {
  if (clauses.length <= 1) return clauses[0] ?? "";
  return `${clauses.slice(0, -1).join(", ")} and ${clauses.at(-1)}`;
}

const sentence = (text) =>
  text ? `${text.charAt(0).toUpperCase()}${text.slice(1)}.` : "";

/**
 * The workspace in a sentence or two, from the numbers core returned, plus
 * the follow-ups worth a click. Groups still loading or unavailable are left
 * out; nothing is said without a number behind it.
 *
 * @param {Record<string, any[] | undefined>} groups panels by group key
 * @returns {{ lead: string, rest: string, actions: { key: string, label: string }[] }}
 */
export function trendsVerdict(groups, days = 30) {
  const d = byId(groups?.decisions);
  const i = byId(groups?.initiatives);
  const l = byId(groups?.loops);
  let lead = "";
  const rest = [];
  const actions = [];

  const backlog = d.ask_backlog;
  const flow = d.asks_flow;
  if (
    isNumber(backlog?.value) &&
    isNumber(backlog?.previous) &&
    backlog.value !== backlog.previous
  )
    lead = `open asks ${backlog.value > backlog.previous ? "rose" : "fell"} from ${formatTrendValue("count", backlog.previous)} to ${formatTrendValue("count", backlog.value)}`;
  else if (isNumber(flow?.raised) && isNumber(flow?.value))
    lead =
      flow.raised > flow.value
        ? `asks are outpacing answers: ${formatTrendValue("count", flow.raised)} raised, ${formatTrendValue("count", flow.value)} answered`
        : `answers are keeping up: ${formatTrendValue("count", flow.value)} answered, ${formatTrendValue("count", flow.raised)} raised`;
  if (isNumber(backlog?.value) && backlog.value > 0)
    actions.push({
      key: "decide",
      label: `Answer ${plural(backlog.value, "open ask")}`,
    });

  const wait = trendDelta(d.answer_time ?? {});
  if (wait && wait.tone !== "flat")
    rest.push(
      `answers got ${wait.tone === "good" ? "faster" : "slower"} (${formatDuration(d.answer_time.value)} typical)`,
    );
  if (isNumber(i.promises?.value))
    rest.push(
      `${formatTrendValue("percent", i.promises.value)} of promises were kept`,
    );
  if (isNumber(i.past_promise?.past) && i.past_promise.past > 0) {
    rest.push(
      `${formatTrendValue("count", i.past_promise.past)} of ${formatTrendValue("count", i.past_promise.total)} initiatives are past their promise`,
    );
    actions.push({
      key: "initiatives",
      label: "See initiatives past their promise",
    });
  }
  const health = l.loop_health;
  if (isNumber(health?.value)) {
    rest.push(
      `${formatTrendValue("percent", health.value)} of loops are reporting on time`,
    );
    if (health.value < 100)
      actions.push({ key: "loops", label: "Check late loops" });
  } else if (isNumber(l.loop_runs?.failed) && l.loop_runs.failed > 0) {
    rest.push(`${plural(l.loop_runs.failed, "loop run")} failed to run`);
    actions.push({ key: "loops", label: "Check loops" });
  }
  if (!lead && rest.length) lead = rest.shift();
  return {
    lead: lead
      ? `${sentence(lead).slice(0, -1)} over the last ${days === 7 ? "week" : `${days} days`}.`
      : "",
    rest: sentence(joinClauses(rest)),
    actions,
  };
}
