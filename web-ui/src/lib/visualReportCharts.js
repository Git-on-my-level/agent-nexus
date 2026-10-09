// A deliberately bounded, data-only ECharts vocabulary. Never forward a report
// option object to the renderer: validate it, then construct owned options below.
export const REPORT_CHART_TYPES = Object.freeze([
  "line",
  "bar",
  "scatter",
  "pie",
  "heatmap",
  "graph",
  "sankey",
  "treemap",
]);
export const REPORT_CHART_LIMITS = Object.freeze({
  series: 12,
  points: 200,
  totalPoints: 1200,
  nodes: 120,
  links: 240,
  depth: 5,
  categories: 12,
  markLines: 6,
  label: 200,
  caption: 2000,
  magnitude: 1e12,
  // Avoid overflowing reciprocal scale factors for subnormal-sized values.
  minNonzeroMagnitude: 1e-100,
  errors: 20,
});
/**
 * Series colours, per palette and per theme.
 *
 * Two rules shape these ramps:
 *
 * 1. **Lightness, not hue alone.** Every ramp descends in relative luminance,
 *    so series stay apart in greyscale, for a colour-blind reader, and in a
 *    printout. This is the live fix: the old ramps put two series 1.01:1 apart
 *    — the same lightness in different hues, which is no separation at all.
 * 2. **Per surface.** Every colour clears 3:1 against the background it is
 *    drawn on (WCAG 1.4.11, the bar for graphical marks).
 *
 * The second rule is groundwork, not a fix for something a reader sees today:
 * `app.css` currently defines one dark token set and the app has no light
 * theme, so only the dark ramps are ever drawn. The old single ramp would have
 * sat at ~1.44:1 on a light surface, which is the bug a light theme would have
 * shipped with. `reportSeriesColors` picks from the actual background, so the
 * day a light theme lands the series colours follow it instead of needing this
 * fixed a second time.
 *
 * The four names are contract — `contracts/visualreport/chart.go` validates
 * them — so the names stay fixed and only the values here change. Each name
 * owns a distinct set of hue families, which is what keeps the four palettes
 * telling apart from one another.
 *
 * `tests/unit/reportChartPalettes.test.js` re-derives every claim above, so a
 * future edit that flattens a ramp fails rather than ships.
 */
export const REPORT_CHART_PALETTES = Object.freeze({
  ocean: Object.freeze({
    dark: ["#a5f3fc", "#c7d2fe", "#c4b5fd", "#14b8a6", "#3b82f6", "#0369a1"],
    light: ["#0891b2", "#0284c7", "#2563eb", "#4f46e5", "#115e59", "#4c1d95"],
  }),
  forest: Object.freeze({
    dark: ["#d9f99d", "#bae6fd", "#fbbf24", "#10b981", "#0d9488", "#4f46e5"],
    light: ["#d97706", "#059669", "#6366f1", "#0f766e", "#3f6212", "#0c4a6e"],
  }),
  sunset: Object.freeze({
    dark: ["#fed7aa", "#f0abfc", "#f59e0b", "#a78bfa", "#f43f5e", "#be185d"],
    light: ["#d97706", "#f43f5e", "#db2777", "#7c3aed", "#9a3412", "#701a75"],
  }),
  categorical: Object.freeze({
    dark: ["#d9f99d", "#f5d0fe", "#7dd3fc", "#f59e0b", "#818cf8", "#be123c"],
    light: ["#65a30d", "#d946ef", "#0284c7", "#b45309", "#be123c", "#3730a3"],
  }),
});

/** WCAG relative luminance of a `#rrggbb` colour. */
export function relativeLuminance(hex) {
  const value = String(hex ?? "").trim();
  if (!/^#[0-9a-f]{6}$/i.test(value)) return NaN;
  const channel = (offset) => {
    const part = Number.parseInt(value.slice(offset, offset + 2), 16) / 255;
    return part <= 0.04045 ? part / 12.92 : ((part + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(1) + 0.7152 * channel(3) + 0.0722 * channel(5);
}

/** WCAG contrast ratio between two `#rrggbb` colours. */
export function contrastRatio(a, b) {
  const first = relativeLuminance(a);
  const second = relativeLuminance(b);
  if (!Number.isFinite(first) || !Number.isFinite(second)) return NaN;
  return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
}

/**
 * Which ramp a surface wants. Derived from the background rather than a theme
 * flag so a workspace that overrides the theme tokens still gets series
 * colours that contrast with whatever surface it actually renders on.
 */
export function reportSeriesColors(palette, background) {
  const ramps = REPORT_CHART_PALETTES[palette] ?? REPORT_CHART_PALETTES.ocean;
  const luminance = relativeLuminance(background);
  const light = Number.isFinite(luminance) ? luminance > 0.5 : false;
  return [...(light ? ramps.light : ramps.dark)];
}
const CARTESIAN = ["line", "bar", "scatter", "heatmap"];
// Literal axis label decoration such as "{value}%" or "$ {value}". ECharts
// interpolates only {value}; braces and markup characters are rejected.
const AXIS_FORMAT = /^[^{}<>]{0,12}\{value\}[^{}<>]{0,12}$/;
const epochMilliseconds = (value) =>
  typeof value === "number" &&
  Number.isFinite(value) &&
  (value === 0 || Math.abs(value) >= REPORT_CHART_LIMITS.minNonzeroMagnitude) &&
  Math.abs(value) <= 8.64e15;
const number = (value) =>
  typeof value === "number" &&
  Number.isFinite(value) &&
  (value === 0 || Math.abs(value) >= REPORT_CHART_LIMITS.minNonzeroMagnitude) &&
  Math.abs(value) <= REPORT_CHART_LIMITS.magnitude;
const record = (value) =>
  value !== null &&
  typeof value === "object" &&
  !Array.isArray(value) &&
  [Object.prototype, null].includes(Object.getPrototypeOf(value));
const axes = (value) =>
  value === undefined ? [] : Array.isArray(value) ? value : [value];

/** Returns bounded diagnostics, without echoing untrusted keys or values. */
export function validateReportChart(data, path = "data") {
  const errors = [];
  const add = (at, message) => {
    if (errors.length < REPORT_CHART_LIMITS.errors)
      errors.push(`${at}: ${message}`);
  };
  const object = (value, at, required, optional = []) => {
    if (!record(value)) {
      add(at, "must be a plain object");
      return false;
    }
    if (required.some((key) => !Object.hasOwn(value, key)))
      add(at, "required fields are missing");
    const allowed = new Set([...required, ...optional]);
    if (
      Object.keys(value).some((key) => !allowed.has(key)) ||
      Object.getOwnPropertySymbols(value).length
    )
      add(at, "contains unsupported fields");
    return true;
  };
  const text = (value, at, max = REPORT_CHART_LIMITS.label, empty = false) => {
    if (
      typeof value !== "string" ||
      value.length > max ||
      (!empty && !value.trim())
    )
      add(
        at,
        `must be ${empty ? "a" : "a nonempty"} string of at most ${max} characters`,
      );
  };
  const numeric = (
    value,
    at,
    min = -REPORT_CHART_LIMITS.magnitude,
    max = REPORT_CHART_LIMITS.magnitude,
  ) => {
    if (!number(value) || value < min || value > max)
      add(at, "must be a finite number in the supported range");
  };
  const enumeration = (value, at, allowed) => {
    if (!allowed.includes(value))
      add(at, `must be one of ${allowed.join(", ")}`);
  };
  const boolean = (value, at) => {
    if (typeof value !== "boolean") add(at, "must be a boolean");
  };
  const array = (value, at, max, min = 0) => {
    if (!Array.isArray(value) || value.length < min || value.length > max) {
      add(at, `must be an array with ${min}–${max} items`);
      return [];
    }
    return value;
  };
  const optional = (value, at, key, check) => {
    if (value[key] !== undefined) check(value[key], `${at}.${key}`);
  };
  const category = (value, at) => {
    if (typeof value === "string") text(value, at);
    else numeric(value, at);
  };
  const percent = (value, at) => {
    if (
      typeof value !== "string" ||
      !/^(?:100|[0-9]{1,2})(?:\.[0-9])?%$/.test(value) ||
      parseFloat(value) > 100
    )
      add(at, "must be a percentage from 0% to 100%");
  };
  if (!object(data, path, ["option"], ["caption", "palette"])) return errors;
  optional(data, path, "caption", (v, p) =>
    text(v, p, REPORT_CHART_LIMITS.caption, true),
  );
  optional(data, path, "palette", (v, p) =>
    enumeration(v, p, Object.keys(REPORT_CHART_PALETTES)),
  );
  const option = data.option;
  const op = `${path}.option`;
  if (!object(option, op, ["series"], ["xAxis", "yAxis", "legend"]))
    return errors;
  if (
    option.legend !== undefined &&
    object(option.legend, `${op}.legend`, [], ["show"])
  )
    optional(option.legend, `${op}.legend`, "show", boolean);
  for (const dimension of ["xAxis", "yAxis"]) {
    array(axes(option[dimension]), `${op}.${dimension}`, 2).forEach(
      (axis, i) => {
        const at = `${op}.${dimension}[${i}]`;
        if (
          !object(
            axis,
            at,
            ["type"],
            [
              "name",
              "data",
              "position",
              "inverse",
              "axisLabel",
              "min",
              "max",
              "scale",
            ],
          )
        )
          return;
        enumeration(axis.type, `${at}.type`, ["category", "value", "time"]);
        optional(axis, at, "name", (v, p) =>
          text(v, p, REPORT_CHART_LIMITS.label, true),
        );
        optional(axis, at, "position", (v, p) =>
          enumeration(
            v,
            p,
            dimension === "xAxis" ? ["top", "bottom"] : ["left", "right"],
          ),
        );
        optional(axis, at, "inverse", boolean);
        if (axis.type === "category") {
          const values = array(
            axis.data,
            `${at}.data`,
            REPORT_CHART_LIMITS.points,
            1,
          );
          values.forEach((v, j) => category(v, `${at}.data[${j}]`));
          if (new Set(values.map(String)).size !== values.length)
            add(`${at}.data`, "category labels must be unique");
          if (["min", "max", "scale"].some((key) => axis[key] !== undefined))
            add(at, "category axes do not accept min, max or scale");
        } else {
          if (axis.data !== undefined)
            add(`${at}.data`, "only category axes accept data");
          const bound = (v, p) => {
            if (axis.type === "time" ? !epochMilliseconds(v) : !number(v))
              add(p, "must be a finite number in the supported range");
          };
          optional(axis, at, "min", bound);
          optional(axis, at, "max", bound);
          optional(axis, at, "scale", boolean);
          if (number(axis.min) && number(axis.max) && axis.min >= axis.max)
            add(at, "min must be smaller than max");
        }
        if (
          axis.axisLabel !== undefined &&
          object(
            axis.axisLabel,
            `${at}.axisLabel`,
            [],
            ["show", "rotate", "formatter"],
          )
        ) {
          optional(axis.axisLabel, `${at}.axisLabel`, "show", boolean);
          optional(axis.axisLabel, `${at}.axisLabel`, "rotate", (v, p) =>
            numeric(v, p, -90, 90),
          );
          optional(axis.axisLabel, `${at}.axisLabel`, "formatter", (v, p) => {
            if (axis.type !== "value")
              add(p, "label formats are supported on value axes only");
            else if (typeof v !== "string" || !AXIS_FORMAT.test(v))
              add(p, "must be literal text around a single {value}");
          });
        }
      },
    );
  }
  let total = 0;
  const series = array(
    option.series,
    `${op}.series`,
    REPORT_CHART_LIMITS.series,
    1,
  );
  const names = new Set();
  series.forEach((item, i) => {
    const at = `${op}.series[${i}]`;
    if (!record(item)) {
      add(at, "must be a plain object");
      return;
    }
    enumeration(item.type, `${at}.type`, REPORT_CHART_TYPES);
    const supported =
      {
        line: [
          "xAxisIndex",
          "yAxisIndex",
          "stack",
          "smooth",
          "step",
          "areaStyle",
          "symbolSize",
          "markLine",
        ],
        bar: ["xAxisIndex", "yAxisIndex", "stack", "barWidth", "markLine"],
        scatter: ["xAxisIndex", "yAxisIndex", "symbolSize", "markLine"],
        pie: ["radius", "center", "roseType"],
        heatmap: ["xAxisIndex", "yAxisIndex"],
        graph: ["layout", "links", "symbolSize", "categories"],
        sankey: ["links", "orient"],
        treemap: [],
      }[item.type] ?? [];
    if (!object(item, at, ["type", "name", "data"], supported)) return;
    text(item.name, `${at}.name`);
    if (names.has(item.name)) add(`${at}.name`, "series names must be unique");
    names.add(item.name);
    optional(item, at, "stack", text);
    optional(item, at, "smooth", boolean);
    optional(item, at, "step", (v, p) =>
      enumeration(v, p, ["start", "middle", "end"]),
    );
    optional(item, at, "symbolSize", (v, p) => numeric(v, p, 2, 40));
    optional(item, at, "barWidth", (v, p) => numeric(v, p, 1, 80));
    if (item.areaStyle !== undefined)
      object(item.areaStyle, `${at}.areaStyle`, []);
    const points = array(
      item.data,
      `${at}.data`,
      item.type === "graph" || item.type === "sankey"
        ? REPORT_CHART_LIMITS.nodes
        : REPORT_CHART_LIMITS.points,
      1,
    );
    total += points.length;
    if (CARTESIAN.includes(item.type)) {
      const selected = ["xAxis", "yAxis"].map((dimension) => {
        const index = item[`${dimension}Index`] ?? 0;
        if (!Number.isInteger(index) || index < 0 || index > 1)
          add(`${at}.${dimension}Index`, "must be 0 or 1");
        const axis = axes(option[dimension])[index];
        if (!record(axis))
          add(at, "cartesian series require matching xAxis and yAxis");
        return axis;
      });
      const coordinate = (value, axis, p) => {
        if (value === null) return;
        if (axis?.type === "category") {
          category(value, p);
          if (
            Array.isArray(axis.data) &&
            !axis.data.map(String).includes(String(value))
          )
            add(p, "must match a category label");
        } else if (axis?.type === "time") {
          if (!epochMilliseconds(value))
            add(p, "time coordinates must be valid epoch milliseconds");
        } else numeric(value, p);
      };
      if (item.type === "heatmap") {
        if (selected.some((axis) => axis?.type !== "category"))
          add(at, "heatmaps require two category axes");
        const cells = new Set();
        points.forEach((point, j) => {
          const p = `${at}.data[${j}]`;
          if (!Array.isArray(point) || point.length !== 3) {
            add(p, "must be a column, row, value tuple");
            return;
          }
          point.slice(0, 2).forEach((v, k) => {
            if (
              !Number.isInteger(v) ||
              v < 0 ||
              v >= (selected[k]?.data?.length ?? 0)
            )
              add(p, "cell indices must match the category axes");
          });
          if (point[2] !== null) numeric(point[2], p);
          const key = `${point[0]},${point[1]}`;
          if (cells.has(key)) add(p, "heatmap cells must be unique");
          cells.add(key);
        });
      } else {
        const categorical = selected.findIndex(
          (axis) => axis?.type === "category",
        );
        const categoryCount = selected.filter(
          (axis) => axis?.type === "category",
        ).length;
        points.forEach((point, j) => {
          const p = `${at}.data[${j}]`;
          if (Array.isArray(point)) {
            if (point.length !== 2) add(p, "must be an x, y tuple");
            else
              point.forEach((v, k) => coordinate(v, selected[k], `${p}[${k}]`));
          } else {
            if (point !== null) numeric(point, p);
            if (categoryCount !== 1)
              add(p, "scalar values require exactly one category axis");
            else if (j >= (selected[categorical]?.data?.length ?? 0))
              add(p, "has no matching axis category");
          }
        });
        if (item.stack !== undefined && categoryCount !== 1)
          add(at, "stacked series require exactly one category axis");
      }
      if (
        item.markLine !== undefined &&
        object(item.markLine, `${at}.markLine`, ["data"])
      )
        array(
          item.markLine.data,
          `${at}.markLine.data`,
          REPORT_CHART_LIMITS.markLines,
          1,
        ).forEach((line, j) => {
          const p = `${at}.markLine.data[${j}]`;
          if (!object(line, p, ["name"], ["xAxis", "yAxis"])) return;
          text(line.name, `${p}.name`);
          const dimensions = ["xAxis", "yAxis"].filter(
            (key) => line[key] !== undefined,
          );
          if (dimensions.length !== 1) {
            add(p, "reference lines require exactly one of xAxis or yAxis");
            return;
          }
          const axis = selected[dimensions[0] === "xAxis" ? 0 : 1];
          if (axis?.type === "value") numeric(line[dimensions[0]], p);
          else if (axis?.type === "time") {
            if (!epochMilliseconds(line[dimensions[0]]))
              add(p, "time coordinates must be valid epoch milliseconds");
          } else add(p, "reference lines require a value or time axis");
        });
    } else if (item.type === "pie") {
      points.forEach((point, j) => {
        const p = `${at}.data[${j}]`;
        if (object(point, p, ["name", "value"])) {
          text(point.name, `${p}.name`);
          numeric(point.value, `${p}.value`, 0);
        }
      });
      if (item.radius !== undefined) {
        if (Array.isArray(item.radius)) {
          array(item.radius, `${at}.radius`, 2, 2).forEach((v) =>
            percent(v, `${at}.radius`),
          );
          if (parseFloat(item.radius[0]) >= parseFloat(item.radius[1]))
            add(
              `${at}.radius`,
              "inner radius must be smaller than outer radius",
            );
        } else percent(item.radius, `${at}.radius`);
      }
      if (item.center !== undefined)
        array(item.center, `${at}.center`, 2, 2).forEach((v) =>
          percent(v, `${at}.center`),
        );
      optional(item, at, "roseType", (v, p) =>
        enumeration(v, p, ["radius", "area"]),
      );
    } else if (item.type === "graph" || item.type === "sankey") {
      const graph = item.type === "graph";
      if (graph) enumeration(item.layout, `${at}.layout`, ["circular", "none"]);
      const categories =
        graph && item.categories !== undefined
          ? array(
              item.categories,
              `${at}.categories`,
              REPORT_CHART_LIMITS.categories,
              1,
            )
          : [];
      const categoryNames = new Set();
      categories.forEach((entry, j) => {
        const p = `${at}.categories[${j}]`;
        if (!object(entry, p, ["name"])) return;
        text(entry.name, `${p}.name`);
        if (categoryNames.has(entry.name))
          add(p, "category names must be unique");
        categoryNames.add(entry.name);
      });
      optional(item, at, "orient", (v, p) =>
        enumeration(v, p, ["horizontal", "vertical"]),
      );
      const ids = new Set();
      points.forEach((point, j) => {
        const p = `${at}.data[${j}]`;
        if (
          !object(
            point,
            p,
            graph ? ["id", "name"] : ["name"],
            graph ? ["value", "x", "y", "symbolSize", "category"] : [],
          )
        )
          return;
        text(point.name, `${p}.name`);
        if (graph) text(point.id, `${p}.id`);
        const id = graph ? point.id : point.name;
        if (ids.has(id)) add(p, "nodes must have unique identifiers");
        ids.add(id);
        optional(point, p, "value", numeric);
        optional(point, p, "symbolSize", (v, q) => numeric(v, q, 2, 40));
        optional(point, p, "x", numeric);
        optional(point, p, "y", numeric);
        optional(point, p, "category", (v, q) => {
          if (!Number.isInteger(v) || v < 0 || v >= categories.length)
            add(q, "must index a declared category");
        });
        if (
          graph &&
          item.layout === "none" &&
          (!number(point.x) || !number(point.y))
        )
          add(p, "fixed graph nodes require x and y coordinates");
      });
      const links = array(item.links, `${at}.links`, REPORT_CHART_LIMITS.links);
      const incoming = new Map([...ids].map((id) => [id, 0]));
      const outgoing = new Map([...ids].map((id) => [id, []]));
      links.forEach((link, j) => {
        const p = `${at}.links[${j}]`;
        if (
          !object(
            link,
            p,
            graph ? ["source", "target"] : ["source", "target", "value"],
            graph ? ["value"] : [],
          )
        )
          return;
        text(link.source, `${p}.source`);
        text(link.target, `${p}.target`);
        if (!ids.has(link.source) || !ids.has(link.target))
          add(p, "links must reference existing nodes");
        optional(link, p, "value", (v, q) => numeric(v, q, 0));
        if (outgoing.has(link.source) && incoming.has(link.target)) {
          outgoing.get(link.source).push(link.target);
          incoming.set(link.target, incoming.get(link.target) + 1);
        }
      });
      if (!graph) {
        if (!links.some((link) => number(link?.value) && link.value > 0))
          add(at, "sankey charts require at least one positive flow");
        const queue = [...incoming]
          .filter(([, count]) => count === 0)
          .map(([id]) => id);
        let visited = 0;
        while (queue.length) {
          const id = queue.pop();
          visited += 1;
          for (const next of outgoing.get(id)) {
            incoming.set(next, incoming.get(next) - 1);
            if (incoming.get(next) === 0) queue.push(next);
          }
        }
        if (visited !== ids.size)
          add(at, "sankey links must not contain cycles");
      }
    } else if (item.type === "treemap") {
      let count = 0;
      const visit = (point, p, depth) => {
        count += 1;
        if (
          count > REPORT_CHART_LIMITS.points ||
          depth > REPORT_CHART_LIMITS.depth
        ) {
          add(p, "tree exceeds the node or depth limit");
          return;
        }
        if (!object(point, p, ["name"], ["value", "children"])) return;
        text(point.name, `${p}.name`);
        optional(point, p, "value", (v, q) => numeric(v, q, 0));
        if (point.children !== undefined) {
          if (point.value !== undefined)
            add(p, "branch values are derived from their children");
          array(
            point.children,
            `${p}.children`,
            REPORT_CHART_LIMITS.points,
            1,
          ).forEach((child, j) =>
            visit(child, `${p}.children[${j}]`, depth + 1),
          );
        } else if (!number(point.value))
          add(p, "leaf nodes require a numeric value");
      };
      points.forEach((point, j) => visit(point, `${at}.data[${j}]`, 1));
      total += count - points.length;
    }
  });
  if (total > REPORT_CHART_LIMITS.totalPoints)
    add(`${op}.series`, "exceeds the total point limit");
  if (
    series.some((s) =>
      ["graph", "sankey", "treemap", "heatmap"].includes(s?.type),
    ) &&
    series.length !== 1
  )
    add(
      `${op}.series`,
      "graph, sankey, treemap and heatmap charts require a single series",
    );
  if (
    series.some((s) => s?.type === "pie") &&
    series.some((s) => s?.type !== "pie")
  )
    add(
      `${op}.series`,
      "pie series cannot share a plot with other series types",
    );
  return errors;
}

const pick = (object, keys) =>
  Object.fromEntries(
    keys
      .filter((key) => object[key] !== undefined)
      .map((key) => [key, object[key]]),
  );
const treeCopy = (node) => ({
  name: node.name,
  ...pick(node, ["value"]),
  ...(node.children ? { children: node.children.map(treeCopy) } : {}),
});

/** Constructs fresh renderer options. This is the only path from report to ECharts. */
export function buildReportChartOption(data, appearance = {}) {
  if (validateReportChart(data).length) return null;
  const input = data.option;
  const theme = {
    fg: "#e8ebf1",
    muted: "#a1a7b4",
    line: "#2a2f3d",
    strong: "#3a4050",
    panel: "#161922",
    bg: "#0b0d12",
  };
  for (const key of Object.keys(theme)) {
    if (
      typeof appearance[key] === "string" &&
      /^#[0-9a-f]{6}$/i.test(appearance[key])
    )
      theme[key] = appearance[key];
  }
  const colors = reportSeriesColors(data.palette ?? "ocean", theme.bg);
  // Legend placement moved out of the canvas; `reportLegendModel` decides
  // whether one renders and what it lists.
  const axisTooltip = input.series.some(
    (series) =>
      ["line", "bar"].includes(series.type) &&
      !series.data.some((point) => Array.isArray(point)),
  );
  const horizontal = axes(input.yAxis)[0]?.type === "category";
  const chartAxis = (axis, index) => ({
    ...pick(axis, ["type", "name", "position", "inverse", "min", "max"]),
    ...(axis.data ? { data: axis.data.map(String) } : {}),
    position: axis.position,
    nameLocation: "middle",
    nameGap: 36,
    nameTextStyle: { color: theme.muted, fontSize: 11 },
    axisLine: { lineStyle: { color: theme.strong } },
    axisTick: { show: false },
    axisLabel: {
      color: theme.muted,
      fontSize: 11,
      hideOverlap: true,
      width: 110,
      overflow: "truncate",
      ...pick(axis.axisLabel ?? {}, ["show", "rotate", "formatter"]),
    },
    splitLine: {
      show: axis.type !== "category" && index === 0,
      lineStyle: { color: theme.line, type: "dashed" },
    },
    scale: axis.scale === true,
  });
  const option = {
    animation: false,
    // Local zone, same as every other time on the page.
    useUTC: false,
    backgroundColor: "transparent",
    color: colors,
    textStyle: { fontFamily: "Inter, sans-serif", color: theme.fg },
    aria: { enabled: true, decal: { show: false } },
    tooltip: {
      trigger: axisTooltip ? "axis" : "item",
      axisPointer: {
        type: "shadow",
        shadowStyle: { color: "rgba(255, 255, 255, 0.04)" },
      },
      // `html` lets the formatter return a built element rather than a string.
      // Series names are agent-authored, so the builder sets `textContent` and
      // never assembles markup: report strings stay data, never code. The old
      // `richText` mode could not draw a swatch or a two-column row at all.
      renderMode: "html",
      appendToBody: false,
      confine: true,
      backgroundColor: theme.panel,
      borderColor: theme.strong,
      textStyle: { color: theme.fg, fontSize: 12 },
    },
    // The legend is rendered in Svelte, outside the canvas: it has to wrap
    // rather than paginate, its items have to be real buttons, and clicking
    // one has to pin and highlight a series instead of hiding it. None of
    // those are things the built-in legend does.
    legend: { show: false },
    grid: {
      top: 28,
      right: 44,
      bottom: 52,
      left: 52,
      outerBoundsMode: "same",
      outerBoundsContain: "all",
    },
    series: input.series.map((series) => {
      const base = { type: series.type, name: series.name, animation: false };
      if (["line", "bar", "scatter", "heatmap"].includes(series.type)) {
        Object.assign(
          base,
          pick(series, [
            "xAxisIndex",
            "yAxisIndex",
            "stack",
            "smooth",
            "step",
            "symbolSize",
            "barWidth",
          ]),
        );
        if (series.markLine)
          base.markLine = {
            silent: true,
            symbol: ["none", "none"],
            lineStyle: { color: theme.muted, type: "dashed", width: 1 },
            label: {
              color: theme.muted,
              fontSize: 10,
              formatter: "{b}",
              position: "insideEndTop",
              backgroundColor: theme.panel,
              padding: [2, 4],
              borderRadius: 3,
            },
            data: series.markLine.data.map((line) =>
              pick(line, ["name", "xAxis", "yAxis"]),
            ),
          };
        if (input.series.length > 1) base.emphasis = { focus: "series" };
        base.data = series.data.map((point) => {
          if (!Array.isArray(point)) return point;
          if (series.type === "heatmap") return [...point];
          const selected = [
            axes(input.xAxis)[series.xAxisIndex ?? 0],
            axes(input.yAxis)[series.yAxisIndex ?? 0],
          ];
          return point.map((value, index) =>
            selected[index]?.type === "category" && value !== null
              ? String(value)
              : value,
          );
        });
        if (series.type === "line") {
          base.connectNulls = false;
          base.showSymbol = series.data.length <= 60;
          base.symbol = "circle";
          base.lineStyle = { width: 2.5 };
          if (series.areaStyle) base.areaStyle = { opacity: 0.17 };
        }
        if (series.type === "bar") {
          base.barMaxWidth = 44;
          if (!series.stack)
            base.itemStyle = {
              borderRadius: horizontal ? [0, 3, 3, 0] : [3, 3, 0, 0],
            };
        }
        if (series.type === "scatter") base.symbolSize = series.symbolSize ?? 9;
        if (series.type === "heatmap") {
          base.data = base.data.filter((point) => point[2] !== null);
          base.itemStyle = {
            borderColor: theme.panel,
            borderWidth: 2,
            borderRadius: 3,
          };
          base.label = {
            show: series.data.length <= 60,
            color: theme.fg,
            fontSize: 10,
            // A soft shadow keeps values legible on both ends of the ramp.
            textShadowColor: "rgba(0, 0, 0, 0.7)",
            textShadowBlur: 3,
          };
        }
      } else if (series.type === "pie") {
        base.data = series.data.map((point) => ({
          name: point.name,
          value: point.value,
        }));
        Object.assign(base, pick(series, ["roseType"]));
        base.radius = Array.isArray(series.radius)
          ? [...series.radius]
          : (series.radius ?? ["42%", "69%"]);
        base.center = series.center ? [...series.center] : ["50%", "45%"];
        base.stillShowZeroSum = false;
        base.avoidLabelOverlap = true;
        base.label = {
          color: theme.muted,
          fontSize: 11,
          width: 95,
          overflow: "truncate",
          formatter: "{b}  {d}%",
        };
        base.itemStyle = {
          borderColor: theme.panel,
          borderWidth: 3,
          borderRadius: 4,
        };
      } else if (series.type === "graph") {
        base.data = series.data.map((node) => ({
          ...pick(node, [
            "id",
            "name",
            "value",
            "x",
            "y",
            "symbolSize",
            "category",
          ]),
        }));
        if (series.categories)
          base.categories = series.categories.map((entry) => ({
            name: entry.name,
          }));
        base.links = series.links.map((link) =>
          pick(link, ["source", "target", "value"]),
        );
        base.layout = series.layout;
        base.symbolSize = series.symbolSize ?? 20;
        base.roam = false;
        base.draggable = false;
        base.edgeSymbol = ["none", "arrow"];
        base.edgeSymbolSize = 7;
        base.lineStyle = { color: theme.muted, opacity: 0.45, curveness: 0.12 };
        base.itemStyle = { borderColor: theme.panel, borderWidth: 2 };
        base.emphasis = {
          focus: "adjacency",
          lineStyle: { color: theme.fg, opacity: 0.8, width: 2 },
        };
        base.label = {
          show: true,
          position: "right",
          color: theme.fg,
          width: 100,
          overflow: "truncate",
          fontSize: 11,
        };
        base.labelLayout = { hideOverlap: true };
        base.top = "14%";
        base.bottom = "10%";
        base.left = "14%";
        base.right = 128;
      } else if (series.type === "sankey") {
        base.data = series.data.map((node) => ({ name: node.name }));
        base.links = series.links.map((link) =>
          pick(link, ["source", "target", "value"]),
        );
        base.orient = series.orient ?? "horizontal";
        base.draggable = false;
        base.layoutIterations = 24;
        // Bound total spacing independently of node count. A fixed gap can
        // exceed the entire plot height and make ECharts emit negative sizes.
        base.nodeWidth = Math.min(16, 120 / series.data.length);
        base.nodeGap = Math.min(18, 60 / series.data.length);
        base.lineStyle = { color: "gradient", opacity: 0.32, curveness: 0.5 };
        base.itemStyle = { borderWidth: 0, borderRadius: 2 };
        base.emphasis = { focus: "adjacency", lineStyle: { opacity: 0.6 } };
        base.label = {
          color: theme.fg,
          fontSize: 11,
          width: 100,
          overflow: "truncate",
        };
        base.left = "3%";
        base.right = 112;
        base.top = "10%";
        base.bottom = "8%";
      } else if (series.type === "treemap") {
        base.data = series.data.map(treeCopy);
        base.roam = false;
        base.nodeClick = false;
        base.breadcrumb = { show: false };
        base.top = 10;
        base.bottom = 4;
        base.left = 0;
        base.right = 0;
        base.label = { color: theme.fg, fontSize: 12, overflow: "truncate" };
        base.upperLabel = {
          show: true,
          height: 24,
          color: theme.fg,
          fontWeight: 600,
        };
        base.itemStyle = {
          borderColor: theme.panel,
          borderWidth: 2,
          gapWidth: 3,
          borderRadius: 3,
        };
        // Each branch keeps its palette hue; siblings step through translucent
        // tints so light labels stay readable and hierarchy stays visible.
        base.levels = [
          { itemStyle: { borderWidth: 0, gapWidth: 6 } },
          {
            colorAlpha: [0.55, 0.85],
            itemStyle: {
              borderColor: theme.panel,
              borderWidth: 3,
              gapWidth: 3,
            },
          },
          { colorAlpha: [0.45, 0.8], itemStyle: { gapWidth: 2 } },
        ];
      }
      return base;
    }),
  };
  if (input.series.some((series) => CARTESIAN.includes(series.type))) {
    option.xAxis = axes(input.xAxis).map(chartAxis);
    option.yAxis = axes(input.yAxis).map(chartAxis);
  }
  if (input.series[0].type === "heatmap") {
    const values = input.series[0].data.map((point) => point[2]).filter(number);
    const min = Math.min(0, ...values),
      max = Math.max(0, ...values);
    option.visualMap = {
      min,
      max: max === min ? min + 1 : max,
      orient: "horizontal",
      left: "center",
      bottom: 0,
      calculable: false,
      itemWidth: 10,
      itemHeight: 160,
      text: [String(max === min ? min + 1 : max), String(min)],
      textGap: 8,
      textStyle: { color: theme.muted, fontSize: 10 },
      inRange: { color: ["#24334d", colors[1], colors[0]] },
    };
    option.legend.show = false;
    option.grid.bottom = 78;
  }
  return option;
}

/** An ordinary semantic table is always available, including without JavaScript. */
export function reportChartRows(data) {
  const columns = ["Series", "Item / relationship", "X", "Y", "Value"];
  if (validateReportChart(data).length) return { columns, rows: [] };
  const rows = [];
  const display = (value) =>
    value === null || value === undefined ? "No observation" : String(value);
  for (const series of data.option.series) {
    const x = axes(data.option.xAxis)[series.xAxisIndex ?? 0];
    const y = axes(data.option.yAxis)[series.yAxisIndex ?? 0];
    if (series.type === "treemap") {
      const visit = (nodes, parents = []) =>
        nodes.forEach((node) => {
          const names = [...parents, node.name];
          rows.push([
            series.name,
            names.join(" / "),
            "",
            "",
            node.value === undefined ? "Sum of children" : display(node.value),
          ]);
          if (node.children) visit(node.children, names);
        });
      visit(series.data);
    } else if (series.type === "sankey" || series.type === "graph") {
      const names = new Map(
        series.data.map((node) => [node.id ?? node.name, node.name]),
      );
      series.data.forEach((node) =>
        rows.push([
          series.name,
          node.category === undefined
            ? node.name
            : `${node.name} (${series.categories[node.category].name})`,
          node.x === undefined ? "" : display(node.x),
          node.y === undefined ? "" : display(node.y),
          node.value === undefined ? "" : display(node.value),
        ]),
      );
      series.links.forEach((link) =>
        rows.push([
          series.name,
          `${names.get(link.source)} → ${names.get(link.target)}`,
          "",
          "",
          link.value === undefined ? "" : display(link.value),
        ]),
      );
    } else {
      series.data.forEach((point, index) => {
        if (series.type === "pie")
          rows.push([series.name, point.name, "", "", display(point.value)]);
        else if (series.type === "heatmap")
          rows.push([
            series.name,
            "",
            display(x.data[point[0]]),
            display(y.data[point[1]]),
            display(point[2]),
          ]);
        else if (Array.isArray(point))
          rows.push([
            series.name,
            String(index + 1),
            display(point[0]),
            display(point[1]),
            "",
          ]);
        else if (y?.type === "category")
          rows.push([
            series.name,
            String(index + 1),
            display(point),
            display(y.data[index]),
            "",
          ]);
        else
          rows.push([
            series.name,
            String(index + 1),
            display(x?.data?.[index] ?? index + 1),
            display(point),
            "",
          ]);
      });
    }
    for (const line of series.markLine?.data ?? [])
      rows.push([
        series.name,
        `Reference: ${line.name}`,
        line.xAxis === undefined ? "" : display(line.xAxis),
        line.yAxis === undefined ? "" : display(line.yAxis),
        "",
      ]);
  }
  return { columns, rows };
}
