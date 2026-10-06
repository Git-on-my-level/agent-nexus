export const seriesPanelTypes = [
  // A timeline is a series binding, not a query: an adapter publishes the
  // events and the panel shows them one by one rather than binned.
  "live-timeline",
  "chart",
  "metric",
  "metric-strip",
  "table",
  "metric-chart",
  "evidence-table",
];
const SERIES_RANGE = /^([1-9][0-9]{0,5})(s|m|h|d)$/;
const RANGE_UNITS = { s: 1, m: 60, h: 3600, d: 86400 };

/** A bounded series duration in seconds, or `null`. Mirrors `SeriesRange`. */
export function seriesRangeSeconds(value) {
  const match = typeof value === "string" && value.match(SERIES_RANGE);
  if (!match) return null;
  const seconds = Number(match[1]) * RANGE_UNITS[match[2]];
  return seconds > 3650 * 86400 ? null : seconds;
}
const name = /^[a-z][a-z0-9_.-]{0,79}$/;
export function validateSeriesBinding(panel) {
  const errors = [];
  const source = panel.source;
  if (!seriesPanelTypes.includes(panel.type))
    errors.push("only charts, metrics, metric-strips and tables bind series");
  if (!source || typeof source !== "object" || Array.isArray(source))
    return [...errors, "source must be an object"];
  if (
    Object.keys(source).some(
      (k) => !["series", "labels", "range", "agg"].includes(k),
    )
  )
    errors.push("source contains unsupported fields");
  if (typeof source.series !== "string" || !name.test(source.series))
    errors.push("source must name a series");
  const range = seriesRangeSeconds(source.range);
  if (source.range !== undefined && range === null)
    errors.push("range must be a bounded duration");
  if (panel.type === "live-timeline") {
    // A timeline keeps every observation, so it is bounded by range alone and
    // has nothing to aggregate.
    if (source.agg !== undefined)
      errors.push(
        "live-timeline preserves observations and does not accept aggregation",
      );
    if (range !== null && range > 90 * 86400)
      errors.push("live-timeline range must not exceed 90d");
  }
  if (
    source.agg !== undefined &&
    !["last", "avg", "sum", "min", "max", "count"].includes(source.agg)
  )
    errors.push("invalid aggregation");
  if (
    source.labels !== undefined &&
    (!source.labels ||
      typeof source.labels !== "object" ||
      Array.isArray(source.labels) ||
      Object.keys(source.labels).length > 8 ||
      Object.entries(source.labels).some(
        ([k, v]) =>
          !name.test(k) ||
          typeof v !== "string" ||
          new TextEncoder().encode(v).length > 128,
      ))
  )
    errors.push("invalid labels");
  if (
    !panel.data ||
    typeof panel.data !== "object" ||
    Array.isArray(panel.data) ||
    Object.keys(panel.data).length
  )
    errors.push(
      "bound panels use empty data and timestamped fallback snapshots",
    );
  // A timeline has no authored snapshot to fall back to: a hand-written list
  // of events is the hand-maintained status this panel type exists to replace.
  if (panel.type === "live-timeline" && panel.fallback !== undefined)
    errors.push("live-timeline does not accept a static fallback");
  return errors;
}

// Query results never inherit authored freshness. Stale/missing observations may
// display a snapshot only with its original as-of time, even while loading.
export function withSeriesObservation(panel, observation) {
  const fallback = panel.fallback;
  const live = observation?.status === "ok";
  return {
    ...panel,
    data: live ? observation.data : (fallback?.data ?? {}),
    observed_at: live ? observation.observed_at : (fallback?.as_of ?? null),
    freshness: live
      ? "current"
      : observation?.status === "stale"
        ? "stale"
        : "unavailable",
    seriesObservation: observation ?? { status: "loading" },
    seriesFallback: !live && !!fallback,
  };
}
