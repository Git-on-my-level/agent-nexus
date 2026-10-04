import { validateReportLayout } from "./visualReportLayout.js";
import { validateReportChart } from "./visualReportCharts.js";
import {
  LIVE_REPORT_TYPES,
  isLivePanel,
  validateLiveQuery,
} from "./liveReports.js";

// A presentation-only format carried in existing core document revisions.
// Never interpret report strings as markup, code, component names, or fetch URLs.
export const VISUAL_REPORT_KIND = "anx.visual-report";
export const VISUAL_REPORT_VERSION = 1;
export const VISUAL_REPORT_TYPES = Object.freeze([
  "explanation",
  "evidence-table",
  "milestone-timeline",
  "dependency-diagram",
  "metric-chart",
  "artifact-preview",
  "chart",
  "metric-strip",
  "callout",
  "comparison",
  ...LIVE_REPORT_TYPES,
]);
export const VISUAL_REPORT_LIMITS = Object.freeze({
  bytes: 128 * 1024,
  projects: 16,
  sources: 64,
  panels: 32,
  title: 200,
  text: 12000,
  cell: 2000,
  url: 2048,
  columns: 12,
  rows: 200,
  milestones: 100,
  nodes: 40,
  edges: 80,
  points: 200,
  metricMagnitude: 1e12,
  errors: 20,
});
export const VISUAL_REPORT_STALE_AFTER_MS = 24 * 60 * 60 * 1000;

const FRESHNESS = ["current", "stale", "unknown", "unavailable"];
const PROVENANCE = ["reported", "verified", "illustrative"];
const STATUS = ["complete", "pending", "unknown"];
const IDENTIFIER = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$/;
const TIMESTAMP =
  /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,3})?(Z|[+-]\d{2}:\d{2})$/;

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function timestampMillis(value) {
  if (typeof value !== "string") return NaN;
  const match = TIMESTAMP.exec(value);
  if (!match) return NaN;
  const [, year, month, day, hour, minute, second, zone] = match;
  const monthNumber = Number(month);
  const yearNumber = Number(year);
  const leap =
    yearNumber % 4 === 0 && (yearNumber % 100 !== 0 || yearNumber % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (
    monthNumber < 1 ||
    monthNumber > 12 ||
    Number(day) < 1 ||
    Number(day) > days[monthNumber - 1] ||
    Number(hour) > 23 ||
    Number(minute) > 59 ||
    Number(second) > 59 ||
    (zone !== "Z" &&
      (Number(zone.slice(1, 3)) > 23 || Number(zone.slice(4)) > 59))
  )
    return NaN;
  return Date.parse(value);
}

/** Only explicit, credential-free HTTP(S) links may be rendered as anchors. */
export function safeReportUrl(value) {
  if (
    typeof value !== "string" ||
    value.length > VISUAL_REPORT_LIMITS.url ||
    !/^https?:\/\//i.test(value) ||
    /\s|\\/.test(value) ||
    [...value].some(
      (character) =>
        character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127,
    )
  )
    return "";
  try {
    const url = new URL(value);
    if (!url.hostname || url.username || url.password) return "";
    return url.href;
  } catch {
    return "";
  }
}

/** Freshness describes observation age, never completion, availability, or health. */
export function getPanelFreshness(panel, now = Date.now()) {
  if (!isRecord(panel)) return "unknown";
  if (isLivePanel(panel) && !panel.live) return "unavailable";
  if (panel.freshness === "unavailable") return "unavailable";
  if (!FRESHNESS.includes(panel.freshness) || panel.freshness === "unknown")
    return "unknown";
  const observed = timestampMillis(panel.observed_at);
  const current = Number(now);
  if (
    !Number.isFinite(observed) ||
    !Number.isFinite(current) ||
    observed > current + 5 * 60 * 1000
  )
    return "unknown";
  if (
    panel.freshness === "stale" ||
    current - observed > VISUAL_REPORT_STALE_AFTER_MS
  )
    return "stale";
  return "current";
}

export function filterReportPanels(
  report,
  projectId = "all",
  freshness = "all",
  now = Date.now(),
) {
  if (!Array.isArray(report?.panels)) return [];
  return report.panels.filter(
    (panel) =>
      (!projectId || projectId === "all" || panel.project_id === projectId) &&
      (!freshness ||
        freshness === "all" ||
        getPanelFreshness(panel, now) === freshness),
  );
}

function validateReport(report) {
  const errors = [];
  const add = (path, message) => {
    if (errors.length < VISUAL_REPORT_LIMITS.errors)
      errors.push(`${path}: ${message}`);
  };
  const record = (value, path, required, optional = []) => {
    if (!isRecord(value)) {
      add(path, "must be an object");
      return false;
    }
    if (required.some((key) => !Object.hasOwn(value, key)))
      add(path, "required fields are missing");
    const allowed = new Set([...required, ...optional]);
    // Do not echo unknown keys or payloads into diagnostics.
    if (Object.keys(value).some((key) => !allowed.has(key)))
      add(path, "contains unsupported fields");
    return true;
  };
  const string = (
    value,
    path,
    max = VISUAL_REPORT_LIMITS.title,
    empty = false,
  ) => {
    if (
      typeof value !== "string" ||
      value.length > max ||
      (!empty && !value.trim())
    ) {
      add(
        path,
        `must be ${empty ? "a" : "a nonempty"} string of at most ${max} characters`,
      );
      return false;
    }
    return true;
  };
  const identifier = (value, path) => {
    if (typeof value !== "string" || !IDENTIFIER.test(value)) {
      add(
        path,
        "must be an identifier of 1–80 letters, digits, dots, underscores, colons, or hyphens",
      );
      return false;
    }
    return true;
  };
  const array = (value, path, max, min = 0) => {
    if (!Array.isArray(value) || value.length < min || value.length > max) {
      add(path, `must be an array with ${min}–${max} items`);
      return [];
    }
    return value;
  };
  const enumeration = (value, path, values) => {
    if (!values.includes(value))
      add(path, `must be one of ${values.join(", ")}`);
  };
  const timestamp = (value, path, nullable = false) => {
    if (nullable && value === null) return;
    if (!Number.isFinite(timestampMillis(value)))
      add(
        path,
        `must be an ISO 8601 timestamp with a timezone${nullable ? " or null" : ""}`,
      );
  };
  const unique = (items, path) => {
    const ids = new Set();
    items.forEach((item, index) => {
      if (!identifier(item?.id, `${path}[${index}].id`)) return;
      if (ids.has(item.id)) add(`${path}[${index}].id`, "must be unique");
      ids.add(item.id);
    });
    return ids;
  };
  const url = (value, path) => {
    if (!safeReportUrl(value))
      add(path, "must be an absolute HTTP(S) URL without credentials");
  };

  if (
    !record(
      report,
      "report",
      [
        "kind",
        "schema_version",
        "title",
        "summary",
        "generated_at",
        "projects",
        "sources",
        "panels",
      ],
      ["layout"],
    )
  )
    return errors;
  if (report.kind !== VISUAL_REPORT_KIND)
    add("report.kind", "unsupported visual report kind");
  if (report.schema_version !== VISUAL_REPORT_VERSION)
    add("report.schema_version", "unsupported version; expected 1");
  string(report.title, "report.title");
  string(report.summary, "report.summary", VISUAL_REPORT_LIMITS.text);
  timestamp(report.generated_at, "report.generated_at");

  const projects = array(
    report.projects,
    "projects",
    VISUAL_REPORT_LIMITS.projects,
    1,
  );
  const projectIds = unique(projects, "projects");
  if (projectIds.has("all"))
    add("projects", "the project identifier all is reserved for filtering");
  projects.forEach((project, index) => {
    const path = `projects[${index}]`;
    if (!record(project, path, ["id", "title", "summary", "outcome"])) return;
    string(project.title, `${path}.title`);
    string(project.summary, `${path}.summary`, VISUAL_REPORT_LIMITS.text);
    string(project.outcome, `${path}.outcome`, VISUAL_REPORT_LIMITS.cell);
  });

  const sources = array(
    report.sources,
    "sources",
    VISUAL_REPORT_LIMITS.sources,
  );
  const sourceIds = unique(sources, "sources");
  sources.forEach((source, index) => {
    const path = `sources[${index}]`;
    if (!record(source, path, ["id", "label", "url", "observed_at", "kind"]))
      return;
    string(source.label, `${path}.label`);
    string(source.kind, `${path}.kind`, 80);
    url(source.url, `${path}.url`);
    timestamp(source.observed_at, `${path}.observed_at`, true);
  });
  const references = (value, path, panelSources = null) => {
    const ids = array(value, path, VISUAL_REPORT_LIMITS.sources);
    const seen = new Set();
    ids.forEach((id, index) => {
      if (!identifier(id, `${path}[${index}]`)) return;
      if (!sourceIds.has(id))
        add(`${path}[${index}]`, "references a missing source");
      if (panelSources && !panelSources.has(id))
        add(`${path}[${index}]`, "must also appear in panel source_ids");
      if (seen.has(id)) add(`${path}[${index}]`, "must be unique");
      seen.add(id);
    });
  };
  const panels = array(report.panels, "panels", VISUAL_REPORT_LIMITS.panels, 1);
  const panelIds = unique(panels, "panels");
  if (report.layout !== undefined) {
    for (const error of validateReportLayout(report.layout, panelIds))
      add("layout", error);
  }
  panels.forEach((panel, index) => {
    const path = `panels[${index}]`;
    if (
      !record(
        panel,
        path,
        [
          "id",
          "project_id",
          "type",
          "title",
          "author",
          "provenance",
          "observed_at",
          "freshness",
          "source_ids",
          "data",
        ],
        ["appearance", "density"],
      )
    )
      return;
    if (!projectIds.has(panel.project_id))
      add(`${path}.project_id`, "references a missing project");
    if (panel.appearance !== undefined)
      enumeration(panel.appearance, `${path}.appearance`, [
        "plain",
        "soft",
        "outlined",
      ]);
    if (panel.density !== undefined)
      enumeration(panel.density, `${path}.density`, ["compact", "comfortable"]);
    string(panel.title, `${path}.title`);
    string(panel.author, `${path}.author`);
    enumeration(panel.type, `${path}.type`, VISUAL_REPORT_TYPES);
    enumeration(panel.provenance, `${path}.provenance`, PROVENANCE);
    enumeration(panel.freshness, `${path}.freshness`, FRESHNESS);
    timestamp(panel.observed_at, `${path}.observed_at`, true);
    if (
      panel.observed_at === null &&
      ["current", "stale"].includes(panel.freshness)
    )
      add(`${path}.freshness`, "requires an observation timestamp");
    references(panel.source_ids, `${path}.source_ids`);
    if (
      panel.provenance === "verified" &&
      (!Array.isArray(panel.source_ids) || !panel.source_ids.length)
    )
      add(`${path}.source_ids`, "verified panels require evidence sources");
    const panelSources = new Set(
      Array.isArray(panel.source_ids) ? panel.source_ids : [],
    );
    const data = panel.data;
    const dataPath = `${path}.data`;
    if (isLivePanel(panel)) {
      for (const error of validateLiveQuery(panel.type, data))
        add(dataPath, error);
      return;
    }
    switch (panel.type) {
      case "chart":
        for (const error of validateReportChart(data)) add(dataPath, error);
        break;
      case "callout":
        if (!record(data, dataPath, ["tone", "text"], ["label"])) break;
        enumeration(data.tone, `${dataPath}.tone`, [
          "info",
          "success",
          "warning",
          "critical",
        ]);
        string(data.text, `${dataPath}.text`, VISUAL_REPORT_LIMITS.text);
        if (data.label !== undefined) string(data.label, `${dataPath}.label`);
        break;
      case "metric-strip":
        if (!record(data, dataPath, ["items"])) break;
        array(data.items, `${dataPath}.items`, 6, 1).forEach(
          (item, itemIndex) => {
            const itemPath = `${dataPath}.items[${itemIndex}]`;
            if (
              !record(
                item,
                itemPath,
                ["label", "value", "detail"],
                ["trend", "trend_label", "tone"],
              )
            )
              return;
            string(item.label, `${itemPath}.label`);
            string(item.value, `${itemPath}.value`, 80);
            string(
              item.detail,
              `${itemPath}.detail`,
              VISUAL_REPORT_LIMITS.cell,
              true,
            );
            if (item.tone !== undefined)
              enumeration(item.tone, `${itemPath}.tone`, [
                "neutral",
                "positive",
                "negative",
              ]);
            if (item.trend !== undefined) {
              array(item.trend, `${itemPath}.trend`, 50, 2).forEach((value) => {
                if (
                  typeof value !== "number" ||
                  !Number.isFinite(value) ||
                  Math.abs(value) > 1e12
                )
                  add(
                    `${itemPath}.trend`,
                    "must contain finite numbers within ±1e12",
                  );
              });
              string(item.trend_label, `${itemPath}.trend_label`);
            } else if (item.trend_label !== undefined)
              add(itemPath, "trend_label requires trend values");
          },
        );
        break;
      case "comparison":
        if (!record(data, dataPath, ["items"])) break;
        array(data.items, `${dataPath}.items`, 4, 2).forEach(
          (item, itemIndex) => {
            const itemPath = `${dataPath}.items[${itemIndex}]`;
            if (
              !record(item, itemPath, [
                "title",
                "summary",
                "verdict",
                "attributes",
              ])
            )
              return;
            string(item.title, `${itemPath}.title`);
            string(
              item.summary,
              `${itemPath}.summary`,
              VISUAL_REPORT_LIMITS.cell,
            );
            enumeration(item.verdict, `${itemPath}.verdict`, [
              "recommended",
              "neutral",
              "caution",
            ]);
            array(item.attributes, `${itemPath}.attributes`, 10, 1).forEach(
              (attribute, index) => {
                if (
                  !record(attribute, `${itemPath}.attributes[${index}]`, [
                    "label",
                    "value",
                  ])
                )
                  return;
                string(
                  attribute.label,
                  `${itemPath}.attributes[${index}].label`,
                );
                string(
                  attribute.value,
                  `${itemPath}.attributes[${index}].value`,
                  VISUAL_REPORT_LIMITS.cell,
                );
              },
            );
          },
        );
        break;
      case "explanation":
        if (record(data, dataPath, ["text"]))
          string(data.text, `${dataPath}.text`, VISUAL_REPORT_LIMITS.text);
        break;
      case "evidence-table": {
        if (!record(data, dataPath, ["columns", "rows"])) break;
        const columns = array(
          data.columns,
          `${dataPath}.columns`,
          VISUAL_REPORT_LIMITS.columns,
          1,
        );
        columns.forEach((column, columnIndex) =>
          string(column, `${dataPath}.columns[${columnIndex}]`),
        );
        array(data.rows, `${dataPath}.rows`, VISUAL_REPORT_LIMITS.rows).forEach(
          (row, rowIndex) => {
            const rowPath = `${dataPath}.rows[${rowIndex}]`;
            if (!record(row, rowPath, ["cells", "source_ids"])) return;
            const cells = array(
              row.cells,
              `${rowPath}.cells`,
              VISUAL_REPORT_LIMITS.columns,
            );
            if (cells.length !== columns.length)
              add(`${rowPath}.cells`, "must match the column count");
            cells.forEach((cell, cellIndex) =>
              string(
                cell,
                `${rowPath}.cells[${cellIndex}]`,
                VISUAL_REPORT_LIMITS.cell,
                true,
              ),
            );
            references(row.source_ids, `${rowPath}.source_ids`, panelSources);
          },
        );
        break;
      }
      case "milestone-timeline":
        if (!record(data, dataPath, ["items"])) break;
        array(
          data.items,
          `${dataPath}.items`,
          VISUAL_REPORT_LIMITS.milestones,
        ).forEach((item, itemIndex) => {
          const itemPath = `${dataPath}.items[${itemIndex}]`;
          if (
            !record(item, itemPath, [
              "label",
              "date",
              "status",
              "detail",
              "source_ids",
            ])
          )
            return;
          string(item.label, `${itemPath}.label`);
          timestamp(item.date, `${itemPath}.date`, true);
          enumeration(item.status, `${itemPath}.status`, STATUS);
          string(item.detail, `${itemPath}.detail`, VISUAL_REPORT_LIMITS.cell);
          references(item.source_ids, `${itemPath}.source_ids`, panelSources);
        });
        break;
      case "dependency-diagram": {
        if (!record(data, dataPath, ["nodes", "edges"])) break;
        const nodes = array(
          data.nodes,
          `${dataPath}.nodes`,
          VISUAL_REPORT_LIMITS.nodes,
        );
        const nodeIds = unique(nodes, `${dataPath}.nodes`);
        nodes.forEach((node, nodeIndex) => {
          const nodePath = `${dataPath}.nodes[${nodeIndex}]`;
          if (!record(node, nodePath, ["id", "label", "status"])) return;
          string(node.label, `${nodePath}.label`);
          enumeration(node.status, `${nodePath}.status`, STATUS);
        });
        const edgeIds = new Set();
        array(
          data.edges,
          `${dataPath}.edges`,
          VISUAL_REPORT_LIMITS.edges,
        ).forEach((edge, edgeIndex) => {
          const edgePath = `${dataPath}.edges[${edgeIndex}]`;
          if (!record(edge, edgePath, ["from", "to", "label"])) return;
          const validFrom = identifier(edge.from, `${edgePath}.from`);
          const validTo = identifier(edge.to, `${edgePath}.to`);
          if (!validFrom || !validTo) return;
          if (!nodeIds.has(edge.from) || !nodeIds.has(edge.to))
            add(edgePath, "references a missing node");
          if (edge.from === edge.to)
            add(edgePath, "must connect distinct nodes");
          const id = `${edge.from}/${edge.to}`;
          if (edgeIds.has(id)) add(edgePath, "must be unique");
          edgeIds.add(id);
          string(
            edge.label,
            `${edgePath}.label`,
            VISUAL_REPORT_LIMITS.title,
            true,
          );
        });
        break;
      }
      case "metric-chart":
        if (
          !record(data, dataPath, ["unit", "label", "points", "illustrative"])
        )
          break;
        string(data.unit, `${dataPath}.unit`, 80);
        string(data.label, `${dataPath}.label`);
        if (typeof data.illustrative !== "boolean")
          add(`${dataPath}.illustrative`, "must be a boolean");
        if (data.illustrative !== (panel.provenance === "illustrative"))
          add(`${dataPath}.illustrative`, "must agree with panel provenance");
        array(
          data.points,
          `${dataPath}.points`,
          VISUAL_REPORT_LIMITS.points,
        ).forEach((point, pointIndex) => {
          const pointPath = `${dataPath}.points[${pointIndex}]`;
          if (!record(point, pointPath, ["label", "value"])) return;
          string(point.label, `${pointPath}.label`);
          if (
            point.value !== null &&
            (typeof point.value !== "number" ||
              !Number.isFinite(point.value) ||
              point.value < 0 ||
              point.value > VISUAL_REPORT_LIMITS.metricMagnitude)
          )
            add(
              `${pointPath}.value`,
              "must be null or a finite number between 0 and 1e12",
            );
        });
        break;
      case "artifact-preview":
        if (
          !record(data, dataPath, ["label", "media_type", "excerpt"], ["url"])
        )
          break;
        string(data.label, `${dataPath}.label`);
        enumeration(data.media_type, `${dataPath}.media_type`, [
          "text/plain",
          "text/markdown",
          "application/json",
        ]);
        string(data.excerpt, `${dataPath}.excerpt`, VISUAL_REPORT_LIMITS.text);
        if (Object.hasOwn(data, "url")) url(data.url, `${dataPath}.url`);
        break;
    }
  });
  return errors;
}

/**
 * Parse raw document text, with no fences, scripts, HTML, or remote dependencies.
 * A recognized but invalid report must retain an inspectable text fallback.
 */
export function parseVisualReport(content) {
  const unrecognized = { recognized: false, report: null, errors: [] };
  if (typeof content !== "string") return unrecognized;
  const looksLikeReport =
    /^\s*\{/.test(content) &&
    /"kind"\s*:\s*"anx\.visual-report[^"\r\n]*"/.test(
      content.slice(0, VISUAL_REPORT_LIMITS.bytes),
    );
  if (
    content.length > VISUAL_REPORT_LIMITS.bytes ||
    new TextEncoder().encode(content).byteLength > VISUAL_REPORT_LIMITS.bytes
  ) {
    return looksLikeReport
      ? {
          recognized: true,
          report: null,
          errors: ["report: exceeds the 128 KiB size limit"],
        }
      : unrecognized;
  }
  let report;
  try {
    report = JSON.parse(content);
  } catch {
    return looksLikeReport
      ? { recognized: true, report: null, errors: ["report: invalid JSON"] }
      : unrecognized;
  }
  if (
    !isRecord(report) ||
    typeof report.kind !== "string" ||
    !report.kind.startsWith(VISUAL_REPORT_KIND)
  )
    return unrecognized;
  let errors;
  try {
    errors = validateReport(report);
  } catch {
    // Malformed data must never interrupt document navigation or leak payloads.
    errors = ["report: could not safely validate this report"];
  }
  return { recognized: true, report: errors.length ? null : report, errors };
}
