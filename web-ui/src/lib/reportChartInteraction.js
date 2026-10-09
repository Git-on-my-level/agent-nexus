/**
 * Chart tooltip and legend behaviour, as data.
 *
 * An axis tooltip that lists every series in chart order makes the reader hunt
 * for the one under the cursor. These models put the hovered series first and
 * tell it with a value, its share of the total and the change from the
 * previous point, then summarise the rest. The legend lives here too, because
 * the built-in one paginates and its clicks hide series rather than
 * highlighting them.
 *
 * Everything that decides *content* is pure so it can be unit-tested without a
 * browser. `buildTooltipElement` is the only part that touches the DOM, and it
 * writes text through `textContent` — series names come from agent-authored
 * reports, so they are never assembled into markup.
 */

import { reportSeriesColors } from "./visualReportCharts.js";

/** Series listed after the lead before they collapse into "+k more". */
export const TOOLTIP_SERIES_LIMIT = 4;

const isNumber = (value) => typeof value === "number" && Number.isFinite(value);

/**
 * ECharts hands scalars for an axis tooltip and `[x, y]` pairs for scatter.
 * Only the measured value is interesting either way.
 *
 * A gap must stay a gap. The chart contract lets a point be `null` for "not
 * observed", and `Number(null)` is `0` — coercing would turn a missing
 * observation into a measured zero, which is the one thing a report must never
 * claim.
 */
function pointValue(value) {
  if (isNumber(value)) return value;
  if (Array.isArray(value)) {
    const last = value[value.length - 1];
    return isNumber(last) ? last : NaN;
  }
  if (value === null || value === undefined || value === "") return NaN;
  const coerced = Number(value);
  return Number.isFinite(coerced) ? coerced : NaN;
}

/**
 * Compact value text. Large counts get a thousands separator; fractions keep
 * enough precision to distinguish neighbouring points without becoming noise.
 */
export function formatChartValue(value) {
  if (!isNumber(value)) return "No value";
  if (Number.isInteger(value))
    return new Intl.NumberFormat("en-US").format(value);
  const magnitude = Math.abs(value);
  const digits = magnitude >= 100 ? 0 : magnitude >= 1 ? 1 : 2;
  return new Intl.NumberFormat("en-US", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(value);
}

/** Signed change with its direction, for the previous-period line. */
export function formatChartDelta(current, previous) {
  if (!isNumber(current) || !isNumber(previous)) return null;
  const change = current - previous;
  if (change === 0) return { direction: "flat", text: "No change" };
  const sign = change > 0 ? "+" : "−";
  const share = previous === 0 ? null : Math.abs(change / previous) * 100;
  const percent =
    share === null
      ? ""
      : ` (${sign}${share >= 10 ? Math.round(share) : share.toFixed(1)}%)`;
  return {
    direction: change > 0 ? "up" : "down",
    text: `${sign}${formatChartValue(Math.abs(change))}${percent} vs previous`,
  };
}

/**
 * What the legend lists. A single-series legend just repeats the panel title,
 * so it only appears when it distinguishes something — the same rule the
 * built-in legend followed, kept so existing reports look unchanged.
 *
 * Pie slices and graph categories are the legend's subject when present;
 * otherwise it lists series.
 *
 * @param {{ option?: object, palette?: string }} data validated chart panel data
 * @param {string} background the surface the chart renders on
 */
export function reportLegendModel(data, background) {
  const series = Array.isArray(data?.option?.series) ? data.option.series : [];
  if (!series.length) return { show: false, items: [] };

  const colors = reportSeriesColors(data?.palette ?? "ocean", background);
  const color = (index) => colors[index % colors.length];
  const first = series[0];

  let items = [];
  if (first.type === "pie") {
    items = (Array.isArray(first.data) ? first.data : []).map(
      (slice, index) => ({
        key: `slice:${index}`,
        name: String(slice?.name ?? `Slice ${index + 1}`),
        color: color(index),
        seriesIndex: 0,
        dataName: String(slice?.name ?? ""),
      }),
    );
  } else if (first.type === "graph" && first.categories !== undefined) {
    items = (Array.isArray(first.categories) ? first.categories : []).map(
      (category, index) => ({
        key: `category:${index}`,
        name: String(category?.name ?? `Group ${index + 1}`),
        color: color(index),
        seriesIndex: 0,
        dataName: String(category?.name ?? ""),
      }),
    );
  } else {
    items = series.map((entry, index) => ({
      key: `series:${index}`,
      name: String(entry?.name ?? `Series ${index + 1}`),
      color: color(index),
      seriesIndex: index,
    }));
  }

  const authored = data?.option?.legend?.show;
  const show = authored ?? items.length > 1;
  return { show: Boolean(show) && items.length > 0, items };
}

/**
 * Normalize ECharts tooltip params into the rows a tooltip shows.
 *
 * @param {object} input
 * @param {Array<object>} input.points ECharts tooltip params (axis trigger gives an array)
 * @param {number|null} [input.hoveredSeriesIndex] series under the cursor, when known
 * @param {Record<number, number>} [input.previous] previous-point value per series index
 * @param {number} [input.limit]
 */
export function reportTooltipModel({
  points = [],
  hoveredSeriesIndex = null,
  previous = {},
  limit = TOOLTIP_SERIES_LIMIT,
} = {}) {
  const rows = (Array.isArray(points) ? points : [])
    .filter(Boolean)
    .map((point) => ({
      seriesIndex: Number(point.seriesIndex ?? 0),
      name: String(point.seriesName ?? point.name ?? ""),
      color: String(point.color ?? ""),
      value: pointValue(point.value),
      dataIndex: Number(point.dataIndex ?? NaN),
    }));

  if (!rows.length) return null;

  const axisLabel = String(
    points[0]?.axisValueLabel ?? points[0]?.axisValue ?? "",
  );

  // Share of total only means something when every value pushes the same way.
  // A mix of positive and negative makes a percentage of the sum misleading,
  // so it is left off rather than shown wrong.
  const measured = rows.filter((row) => isNumber(row.value));
  const sameSign =
    measured.length > 0 &&
    (measured.every((row) => row.value >= 0) ||
      measured.every((row) => row.value <= 0));
  const total = measured.reduce((sum, row) => sum + row.value, 0);
  const shareOf = (value) =>
    sameSign && total !== 0 && isNumber(value) ? (value / total) * 100 : null;

  // Lead with the series under the cursor. With no cursor hint, the largest
  // value at this point is the one the reader is most likely asking about.
  let lead = rows.find((row) => row.seriesIndex === hoveredSeriesIndex);
  if (!lead) {
    lead = measured.reduce(
      (best, row) => (!best || row.value > best.value ? row : best),
      null,
    );
  }
  if (!lead) [lead] = rows;

  const rest = rows.filter((row) => row !== lead);
  const shown = rest.slice(0, Math.max(0, limit));

  return {
    axisLabel,
    lead: {
      ...lead,
      valueText: formatChartValue(lead.value),
      share: shareOf(lead.value),
      delta: formatChartDelta(lead.value, previous?.[lead.seriesIndex]),
    },
    rest: shown.map((row) => ({
      ...row,
      valueText: formatChartValue(row.value),
      share: shareOf(row.value),
    })),
    more: rest.length - shown.length,
  };
}

/**
 * Previous-point value per series, for the delta line.
 *
 * "Previous period" is only meaningful where the points are ordered, so this
 * reads the point before the hovered one in the same series. A chart whose x
 * axis is unordered gets no delta rather than an invented baseline.
 *
 * @param {Array<object>} series validated chart series
 * @param {number} dataIndex the hovered point
 */
export function previousPointValues(series, dataIndex) {
  const index = Number(dataIndex);
  if (!Array.isArray(series) || !Number.isInteger(index) || index <= 0) {
    return {};
  }
  const previous = {};
  series.forEach((entry, seriesIndex) => {
    const data = Array.isArray(entry?.data) ? entry.data : [];
    const value = pointValue(data[index - 1]);
    if (isNumber(value)) previous[seriesIndex] = value;
  });
  return previous;
}

/**
 * Build the tooltip element. Text is set with `textContent`, so an authored
 * series name cannot introduce markup.
 *
 * @param {ReturnType<typeof reportTooltipModel>} model
 * @param {{ fg?: string, muted?: string }} [theme]
 * @param {Document} [doc]
 */
export function buildTooltipElement(
  model,
  theme = {},
  doc = globalThis.document,
) {
  if (!model || !doc) return null;
  const root = doc.createElement("div");
  root.className = "report-tooltip";

  const swatch = (color) => {
    const dot = doc.createElement("span");
    dot.className = "report-tooltip__swatch";
    dot.style.background = color || "transparent";
    return dot;
  };
  const text = (className, value) => {
    const node = doc.createElement("span");
    node.className = className;
    node.textContent = value;
    return node;
  };

  if (model.axisLabel) {
    root.append(text("report-tooltip__axis", model.axisLabel));
  }

  const lead = doc.createElement("div");
  lead.className = "report-tooltip__lead";
  lead.append(swatch(model.lead.color));
  lead.append(text("report-tooltip__name", model.lead.name || "Series"));
  lead.append(text("report-tooltip__value", model.lead.valueText));
  if (model.lead.share !== null) {
    lead.append(
      text(
        "report-tooltip__share",
        `${model.lead.share.toFixed(model.lead.share >= 10 ? 0 : 1)}% of total`,
      ),
    );
  }
  root.append(lead);

  if (model.lead.delta) {
    const delta = text("report-tooltip__delta", model.lead.delta.text);
    delta.dataset.direction = model.lead.delta.direction;
    root.append(delta);
  }

  if (model.rest.length) {
    const list = doc.createElement("ul");
    list.className = "report-tooltip__rest";
    for (const row of model.rest) {
      const item = doc.createElement("li");
      item.append(swatch(row.color));
      item.append(text("report-tooltip__name", row.name || "Series"));
      item.append(text("report-tooltip__value", row.valueText));
      list.append(item);
    }
    root.append(list);
  }

  if (model.more > 0) {
    root.append(text("report-tooltip__more", `+${model.more} more`));
  }

  if (theme.fg) root.style.color = theme.fg;
  return root;
}
