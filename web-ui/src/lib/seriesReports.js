export const seriesPanelTypes = [
  // A timeline is a series binding, not a query: an adapter publishes the
  // events and the panel shows them one by one rather than binned.
  "live-timeline",
  "live-table",
  "chart",
  "metric",
  "metric-strip",
  "table",
  "metric-chart",
  "evidence-table",
];
// Bounds of the pivoted live-table binding. Mirrors MaxPivotColumns and
// MaxPivotRowKeys in contracts/visualreport/series.go; with both maxed the
// twelve-column table limit is met exactly.
const PIVOT_LIMITS = { columns: 8, rowKeys: 4 };
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
/** A series or label key. Mirrors `seriesName` in contracts/visualreport. */
export const seriesNamePattern = name;
export const AGGREGATIONS = ["last", "avg", "sum", "min", "max", "count"];
export function validateSeriesBinding(panel) {
  const errors = [];
  const source = panel.source;
  if (!seriesPanelTypes.includes(panel.type))
    errors.push("only charts, metrics, metric-strips and tables bind series");
  if (!source || typeof source !== "object" || Array.isArray(source))
    return [...errors, "source must be an object"];
  const pivot = panel.type === "live-table";
  const allowed = pivot
    ? ["columns", "row_keys", "labels", "range"]
    : ["series", "labels", "range", "agg"];
  if (Object.keys(source).some((k) => !allowed.includes(k)))
    errors.push("source contains unsupported fields");
  if (pivot) validatePivotSource(source, errors);
  else {
    if (typeof source.series !== "string" || !name.test(source.series))
      errors.push("source must name a series");
    if (source.agg !== undefined && !AGGREGATIONS.includes(source.agg))
      errors.push("invalid aggregation");
    if (panel.type === "live-timeline") {
      // A timeline keeps every observation, so it is bounded by range alone and
      // has nothing to aggregate.
      if (source.agg !== undefined)
        errors.push(
          "live-timeline preserves observations and does not accept aggregation",
        );
      if (
        source.range !== undefined &&
        seriesRangeSeconds(source.range) > 90 * 86400
      )
        errors.push("live-timeline range must not exceed 90d");
    }
  }
  const range = seriesRangeSeconds(source.range);
  if (source.range !== undefined && range === null)
    errors.push("range must be a bounded duration");
  validateSourceLabels(source, errors);
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

/**
 * The pivoted live-table shape: one column per series and the label keys whose
 * values name the rows. Mirrors `pivotBinding` in
 * contracts/visualreport/series.go.
 */
function validatePivotSource(source, errors) {
  let columns = source.columns;
  if (
    !Array.isArray(columns) ||
    columns.length < 1 ||
    columns.length > PIVOT_LIMITS.columns
  ) {
    errors.push(
      `columns must be an array with 1–${PIVOT_LIMITS.columns} items`,
    );
    columns = Array.isArray(columns) ? columns : [];
  }
  const seen = new Set();
  for (const column of columns) {
    if (!column || typeof column !== "object" || Array.isArray(column)) {
      errors.push("each column must be an object");
      continue;
    }
    if (
      Object.keys(column).some((k) => !["series", "label", "agg"].includes(k))
    )
      errors.push("column contains unsupported fields");
    if (typeof column.series !== "string" || !name.test(column.series))
      errors.push("column must name a series");
    if (column.label !== undefined && !isShortText(column.label))
      errors.push(
        "column label must be a nonempty string of at most 200 characters",
      );
    if (column.agg !== undefined && !AGGREGATIONS.includes(column.agg))
      errors.push("invalid aggregation");
    const key = `${column.series}\u0000${column.agg ?? ""}`;
    if (seen.has(key))
      errors.push("columns must be unique per series and aggregation");
    seen.add(key);
  }
  const rowKeys = Array.isArray(source.row_keys) ? source.row_keys : [];
  if (
    !Array.isArray(source.row_keys) ||
    source.row_keys.length < 1 ||
    source.row_keys.length > PIVOT_LIMITS.rowKeys
  ) {
    errors.push(
      `row_keys must be an array with 1–${PIVOT_LIMITS.rowKeys} items`,
    );
  }
  const keys = new Set();
  for (const rowKey of rowKeys) {
    if (!rowKey || typeof rowKey !== "object" || Array.isArray(rowKey)) {
      errors.push("each row key must be an object");
      continue;
    }
    if (Object.keys(rowKey).some((k) => !["key", "label"].includes(k)))
      errors.push("row key contains unsupported fields");
    if (typeof rowKey.key !== "string" || !name.test(rowKey.key))
      errors.push("row key must be a label key");
    if (keys.has(rowKey.key)) errors.push("row key must be unique");
    keys.add(rowKey.key);
    if (rowKey.label !== undefined && !isShortText(rowKey.label))
      errors.push(
        "row key label must be a nonempty string of at most 200 characters",
      );
  }
}

function isShortText(value) {
  return typeof value === "string" && !!value.trim() && value.length <= 200;
}

function validateSourceLabels(source, errors) {
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
