export const seriesPanelTypes = [
  "chart",
  "metric",
  "metric-strip",
  "table",
  "metric-chart",
  "evidence-table",
];
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
  if (source.range !== undefined) {
    const m =
      typeof source.range === "string" &&
      source.range.match(/^([1-9][0-9]{0,5})(s|m|h|d)$/);
    if (
      !m ||
      Number(m[1]) * { s: 1, m: 60, h: 3600, d: 86400 }[m[2]] > 3650 * 86400
    )
      errors.push("range must be a bounded duration");
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
