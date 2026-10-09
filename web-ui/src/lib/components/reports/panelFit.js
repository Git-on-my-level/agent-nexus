/**
 * How much width a panel needs to read, computed from its type and the content
 * it actually has.
 *
 * This is the whole point of responsive-by-default placement: the renderer
 * decides columns from available width and these numbers, so a report does not
 * have to name a column count. Authored placement stays possible and stays an
 * override (see `reportLayout.js`), but nothing has to supply one.
 *
 * Three facts come out of a panel, and each one answers a different question:
 *
 * - `min`: the narrowest column this panel still reads in. A row only gets
 *   more columns when every panel in it clears its own minimum.
 * - `wide`: this panel's content is wider than a column — a plan graph, a
 *   chart, a table with real columns in it. It claims the full row rather than
 *   being squeezed into half of one and clipped.
 * - `sparse`: there is nearly nothing in it. A nearly-empty panel never claims
 *   a wide row and never raises its row's minimum, so it cannot push its
 *   neighbours into fewer columns than they could have had.
 *
 * Content, not just type, decides `wide` and `sparse`: a two-column table is
 * not wide, a plan with no steps is not a graph, and a live panel that came
 * back with nothing is not a table. A live panel still loading keeps its
 * type's placement so the grid does not reflow under the reader when the
 * observation lands.
 */

/** The column widths a panel's content reads at, narrowest first. */
export const PANEL_FIT_WIDTH = Object.freeze({
  tight: 260,
  text: 380,
  wide: 640,
});

/** Tier order, so a container can take the widest need of its children. */
export const PANEL_FIT_TIERS = Object.freeze(["tight", "text", "wide"]);

/** A number or a short list: never worth a wide column. */
const TIGHT_TYPES = new Set(["metric"]);

/**
 * Types whose populated form is wider than a column. Each one is re-checked
 * against its content below, because a type says what a panel could be and
 * only its data says what it is.
 */
const WIDE_TYPES = new Set([
  "chart",
  "metric-chart",
  "table",
  "evidence-table",
  "dependency-diagram",
  "live-initiatives",
  "live-timeline",
  "live-fleet-health",
]);

/** Prose this short is a label, not a column's worth of reading. */
const SHORT_TEXT = 120;

const list = (value) => (Array.isArray(value) ? value : []);
const text = (value) => (typeof value === "string" ? value.trim() : "");

/**
 * How much there is in a panel, in whatever unit that panel counts in, or
 * `null` when the type has nothing countable.
 *
 * Zero is a definite answer — an empty table, a plan with no steps, a live
 * read that returned no rows — and it is the answer `sparse` is looking for.
 */
function contentCount(type, data) {
  switch (type) {
    case "explanation":
      return text(data.text).length;
    case "callout":
      return text(data.text).length;
    case "artifact-preview":
      return text(data.excerpt).length;
    case "table":
    case "evidence-table":
      return list(data.rows).length;
    case "milestone-timeline":
    case "metric-strip":
    case "comparison":
      return list(data.items).length;
    case "metric-chart":
      return list(data.points).length;
    case "dependency-diagram":
      return list(data.nodes).length;
    case "chart":
      return list(data.option?.series).length || null;
    case "live-initiatives":
    case "live-asks":
    case "live-cards":
    case "live-activity":
    case "live-timeline":
      return list(data.items).length;
    case "live-work-mix":
      return list(data.buckets).length;
    case "live-fleet-health":
      return list(data.hosts).length + list(data.series).length;
    default:
      return null;
  }
}

/** Whether a wide-by-type panel is in fact small enough to share a row. */
function narrowEnough(type, data, count) {
  switch (type) {
    case "table":
    case "evidence-table":
      return list(data.columns).length <= 3 && count <= 6;
    case "metric-chart":
      return count <= 12;
    case "dependency-diagram":
      return count <= 3;
    case "live-fleet-health":
      return count <= 3;
    case "live-initiatives":
      // A plan graph is the wide thing here. Initiative cards without plans
      // are a list, and a list shares a row happily.
      return !list(data.items).some(
        (item) => list(item?.plan?.steps).length > 1,
      );
    default:
      // Charts and timelines are wide whenever they have anything in them.
      return false;
  }
}

/**
 * What a panel is actually rendering, or `null` when there is nothing to
 * measure yet.
 *
 * Three different places hold it, and reading the wrong one is the difference
 * between "nearly empty" and "a full table":
 *
 * - A query-backed live panel keeps its rows on `panel.live.data`
 *   (`withLiveObservation`); `panel.data` is still the authored query.
 * - A series-bound panel has its rows merged onto `panel.data`
 *   (`withSeriesObservation`) — but only for an `ok` read. Anything else
 *   blanks `panel.data` to `{}`, so measuring it would read a waiting panel
 *   as an empty one. A `stale` series still renders, from the observation,
 *   exactly as `SeriesReportPanel` does.
 * - Everything else authored its own data.
 *
 * `null` means "do not reflow for this": a panel that has not answered keeps
 * its type's placement rather than jumping when the rows land.
 */
function renderedData(panel) {
  const live = panel?.live ?? null;
  if (live) return live.status === "ok" ? (live.data ?? {}) : null;
  if (panel?.source) {
    const observation = panel.seriesObservation;
    if (observation?.status === "stale") return observation.data ?? {};
    if (panel.seriesFallback || observation?.status === "ok")
      return panel.data ?? {};
    return null;
  }
  return panel?.data ?? {};
}

/**
 * A panel's placement needs.
 *
 * @param {object|null|undefined} panel an observed report panel
 * @returns {{min: number, tier: string, wide: boolean, sparse: boolean}}
 */
export function panelFit(panel) {
  const type = text(panel?.type);
  const rendered = renderedData(panel);
  const data = rendered ?? {};
  const count =
    rendered && typeof rendered === "object" && !Array.isArray(rendered)
      ? contentCount(type, rendered)
      : null;
  const shortText =
    (type === "explanation" ||
      type === "callout" ||
      type === "artifact-preview") &&
    count !== null &&
    count < SHORT_TEXT;
  const sparse = count === 0 || shortText;
  // Nothing countable (a chart, or a read that has not answered) keeps the
  // type's placement; `narrowEnough` only has an opinion about content it can
  // actually see.
  const wide =
    WIDE_TYPES.has(type) &&
    !sparse &&
    (count === null || !narrowEnough(type, data, count));
  const tier =
    sparse || TIGHT_TYPES.has(type) ? "tight" : wide ? "wide" : "text";
  return { min: PANEL_FIT_WIDTH[tier], tier, wide, sparse };
}

/** The wider of two tiers. */
export function widerTier(a, b) {
  return PANEL_FIT_TIERS.indexOf(a) >= PANEL_FIT_TIERS.indexOf(b) ? a : b;
}
