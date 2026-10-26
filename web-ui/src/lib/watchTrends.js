/**
 * Watch → Trends: presentation for `watch_trends.v1`.
 *
 * Core computes every number; this module only words them. A takeaway is one
 * plain sentence built from a panel's own value and previous value, and says
 * "not enough history" rather than guessing when either is unknown.
 */

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
