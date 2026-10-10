/**
 * A series-bound chart, drawn the way its author asked for.
 *
 * Core materializes a bound `chart` panel the only way it can from raw
 * points: one `line` series per label set, named `"<series> key=value"`, on a
 * time axis. That is a reasonable default and a poor chart. A panel whose
 * author declared bars got lines, a panel whose author declared a stack got
 * neither, and a legend that should read "OSS" and "SaaS" read
 * "anx-prs-merged repo=oss" and "anx-prs-merged repo=saas".
 *
 * Both answers are already on the client. A bound panel may carry a
 * `fallback` — the authored snapshot shown when the publisher goes quiet —
 * and that snapshot is a full chart: its series names, types, stacking and
 * palette are the author's declaration of what this panel is. This applies
 * them to the live read. It is presentation only: no value is invented,
 * reordered or dropped, and a panel with no fallback keeps core's shape with
 * nothing but its legend made readable.
 *
 * The other thing core cannot say is which bucket is still filling. Its last
 * bin ends in the future, so a daily count read at 09:00 plots two hours of
 * today against whole days behind it and the line falls off a cliff. Nothing
 * is wrong, and nobody reading the chart can tell.
 */

import { bucketLabels } from "./time/format.js";
import { REPORT_CHART_LIMITS } from "./visualReportCharts.js";

/** Series fields that are presentation and safe to carry over per type. */
const CARRIED = {
  line: ["stack", "smooth", "step", "areaStyle", "symbolSize"],
  bar: ["stack", "barWidth"],
  scatter: ["symbolSize"],
};
/** The declared types this applies to. Anything else keeps core's shape. */
const SUPPORTED = Object.keys(CARRIED);

const record = (value) =>
  Boolean(value) && typeof value === "object" && !Array.isArray(value);

const axisList = (value) =>
  value === undefined ? [] : Array.isArray(value) ? value : [value];

/**
 * The human name behind `"anx-prs-merged repo=oss"`.
 *
 * Core builds a stream's name by appending its label pairs to the series
 * name, so the series name is a prefix every stream in the panel shares and
 * the label values are the only part that tells two streams apart. Dropping
 * the shared prefix is the whole trick: `repo=oss` → `oss`.
 *
 * An author who wants "OSS" writes it as a series name in the panel's
 * fallback; this is the floor, not the ceiling.
 */
export function seriesStreamLabel(name, seriesName) {
  const full = String(name ?? "").trim();
  const prefix = String(seriesName ?? "").trim();
  if (!full) return "";
  if (!prefix || full === prefix || !full.startsWith(`${prefix} `)) return full;
  const pairs = full
    .slice(prefix.length + 1)
    .split(" ")
    .map((pair) => {
      const at = pair.indexOf("=");
      return at === -1 ? pair : pair.slice(at + 1);
    })
    .filter(Boolean);
  return pairs.length ? pairs.join(" · ") : full;
}

/**
 * How wide one bucket is, in milliseconds.
 *
 * Read from the data rather than recomputed from the panel's range: core
 * rounds its step up and may fall back to whole days under load, so the gaps
 * it actually returned are the only honest answer. The smallest gap wins —
 * a stream missing a bucket leaves a double-width gap, which would otherwise
 * read as a step twice the real one.
 */
export function seriesBucketMs(series) {
  let step = null;
  for (const entry of series) {
    const stamps = (Array.isArray(entry?.data) ? entry.data : [])
      .filter((point) => Array.isArray(point) && Number.isFinite(point[0]))
      .map((point) => point[0])
      .sort((a, b) => a - b);
    for (let i = 1; i < stamps.length; i += 1) {
      const gap = stamps[i] - stamps[i - 1];
      if (gap > 0 && (step === null || gap < step)) step = gap;
    }
  }
  return step;
}

/**
 * The start of the bucket that is still filling, or `null`.
 *
 * The last bucket covers `[start, start + step)`. It is partial exactly when
 * `now` is inside it — which is the usual case for a live series, and never
 * the case for one whose publisher stopped yesterday.
 */
export function partialBucketStart(series, now) {
  const step = seriesBucketMs(series);
  if (!step) return null;
  let last = null;
  for (const entry of series)
    for (const point of Array.isArray(entry?.data) ? entry.data : [])
      if (Array.isArray(point) && Number.isFinite(point[0]))
        last = last === null ? point[0] : Math.max(last, point[0]);
  if (last === null) return null;
  return now >= last && now < last + step ? last : null;
}

/**
 * The authored declaration behind a bound panel, matched to the live streams.
 *
 * Matching is by name first and by position second: an author who named the
 * series exactly as core does gets an exact match, and one who wrote "OSS"
 * and "SaaS" in the order the labels sort gets them in that order. Order is
 * stable — core sorts its label sets — so position is a real answer here
 * rather than a guess.
 */
function authoredSeries(panel) {
  const option = panel?.fallback?.data?.option;
  if (!record(option) || !Array.isArray(option.series)) return null;
  const declared = option.series.filter(
    (entry) => record(entry) && SUPPORTED.includes(entry.type),
  );
  return declared.length ? { option, declared } : null;
}

/**
 * Apply a bound chart panel's authored presentation to its live data.
 *
 * @param {object} panel a series-bound panel after `withSeriesObservation`
 * @param {{now?: number}} [options]
 * @returns {object} chart data for `ReportChart`
 */
export function seriesChartData(panel, { now = Date.now() } = {}) {
  const data = panel?.data;
  // The authored snapshot is already the authored chart; a panel core could
  // not materialize has nothing to restyle.
  if (panel?.seriesFallback || !record(data) || !record(data.option))
    return data;
  const live = data.option;
  if (!Array.isArray(live.series) || !live.series.length) return data;

  const seriesName = String(panel?.source?.series ?? "").trim();
  const authored = authoredSeries(panel);
  const declaredFor = (index, name) =>
    authored
      ? (authored.declared.find((entry) => entry.name === name) ??
        authored.declared[index] ??
        null)
      : null;

  let series = live.series.map((entry, index) => {
    const declared = declaredFor(index, entry.name);
    const type = declared?.type ?? entry.type;
    const next = {
      type,
      // The author's word for this stream, then the stream's own labels, then
      // whatever core called it.
      name:
        declared?.name ||
        seriesStreamLabel(entry.name, seriesName) ||
        entry.name,
      data: entry.data,
    };
    for (const key of CARRIED[type] ?? [])
      if (declared?.[key] !== undefined) next[key] = declared[key];
    return next;
  });
  // Two streams whose labels reduce to the same word would be one series to
  // the validator. Keep core's names rather than merging two lines into one.
  if (new Set(series.map((entry) => entry.name)).size !== series.length)
    series = series.map((entry, index) => ({
      ...entry,
      name: live.series[index].name,
    }));

  const wantsCategories = series.some(
    (entry) => entry.type === "bar" || entry.stack !== undefined,
  );
  const partialAt = partialBucketStart(live.series, now);
  let option = { ...live, series };

  if (wantsCategories && axisList(live.xAxis)[0]?.type === "time") {
    /*
     * Bars and stacks are categorical: this repo's chart contract rejects a
     * stack without a category axis, and ECharts draws hairline bars on a
     * time axis whose points are days apart. Rebinning onto the buckets core
     * already returned changes no value — every series keeps one entry per
     * bucket, `null` where it had no point.
     */
    const stamps = [
      ...new Set(
        series.flatMap((entry) =>
          (Array.isArray(entry.data) ? entry.data : [])
            .filter((point) => Array.isArray(point))
            .map((point) => point[0]),
        ),
      ),
    ].sort((a, b) => a - b);
    /*
     * Rebinning fills every bucket for every stream, so the point count
     * becomes `streams × buckets` where before it was the points each stream
     * actually had. Past either limit the chart would not validate at all,
     * and an unrendered chart is worse than a line one.
     */
    const affordable =
      stamps.length <= REPORT_CHART_LIMITS.points &&
      series.length * stamps.length <= REPORT_CHART_LIMITS.totalPoints;
    const labels = affordable ? bucketLabels(stamps) : null;
    if (labels) {
      // The bucket still filling, said in the axis rather than drawn: a
      // category axis takes no reference line.
      if (partialAt !== null && stamps[stamps.length - 1] === partialAt)
        labels[labels.length - 1] = `${labels[labels.length - 1]} (so far)`;
      option = {
        ...option,
        xAxis: [
          {
            ...axisList(live.xAxis)[0],
            type: "category",
            data: labels,
            // Only a value axis accepts a formatter, and `min`/`max`/`scale`
            // are rejected outright on a category axis.
            axisLabel: undefined,
            min: undefined,
            max: undefined,
            scale: undefined,
          },
        ].map((axis) =>
          Object.fromEntries(
            Object.entries(axis).filter(([, value]) => value !== undefined),
          ),
        ),
        series: series.map((entry) => {
          const byStamp = new Map(
            (Array.isArray(entry.data) ? entry.data : [])
              .filter((point) => Array.isArray(point))
              .map((point) => [point[0], point[1]]),
          );
          return {
            ...entry,
            data: stamps.map((stamp) => byStamp.get(stamp) ?? null),
          };
        }),
      };
    } else {
      // No honest category labels, so no stack either: the contract and the
      // chart both need the category axis the labels would have made.
      option = {
        ...option,
        series: series.map((entry) => {
          if (entry.stack === undefined) return entry;
          const next = { ...entry };
          delete next.stack;
          return next;
        }),
      };
    }
  } else if (partialAt !== null && axisList(live.xAxis)[0]?.type === "time") {
    /*
     * One reference line on the first series, not one per series: they would
     * land on the same instant and draw the same line N times.
     */
    const first = option.series[0];
    const lines = [{ name: "Partial", xAxis: partialAt }];
    if (
      SUPPORTED.includes(first.type) &&
      lines.length <= REPORT_CHART_LIMITS.markLines
    )
      option = {
        ...option,
        series: [
          { ...first, markLine: { data: lines } },
          ...option.series.slice(1),
        ],
      };
  }

  // The author's palette, caption and legend choice belong to the panel, not
  // to the snapshot that happened to carry them.
  const snapshot = panel?.fallback?.data;
  return {
    ...data,
    ...(snapshot?.palette ? { palette: snapshot.palette } : {}),
    ...(snapshot?.caption ? { caption: snapshot.caption } : {}),
    option:
      authored?.option.legend !== undefined
        ? { ...option, legend: authored.option.legend }
        : option,
  };
}
