import { withSeriesObservation } from "./seriesReports.js";
import { formatAgeSeconds } from "./time/format.js";

export const LIVE_REPORT_TYPES = Object.freeze([
  "live-initiatives",
  "live-asks",
  "live-work-mix",
  "live-activity",
  "live-fleet-health",
  "live-cards",
  // Bound to a series rather than a query: its data comes from `source`, so it
  // is here for the panel-type list and never reaches `validateLiveQuery`.
  "live-timeline",
]);

/** The live types that carry a query. `live-timeline` binds a series instead. */
export const LIVE_QUERY_TYPES = Object.freeze(
  LIVE_REPORT_TYPES.filter((type) => type !== "live-timeline"),
);
/**
 * Whether this panel is computed at read time.
 *
 * Prefix rather than list membership, deliberately: `LIVE_REPORT_TYPES` is the
 * validation gate and only core can widen it, so a type this build has never
 * heard of cannot reach a renderer anyway. But the moment core does add one —
 * `live-cards`, `live-timeline` — an older UI treats it as live data with a
 * live provenance line instead of rendering an authored panel with no body.
 */
export const isLivePanel = (panel) =>
  !!panel?.source ||
  (typeof panel?.type === "string" && panel.type.startsWith("live-"));

const UTF8 = new TextEncoder();
/** Byte length, the way a Go `len(string)` counts it. */
const utf8Length = (value) => UTF8.encode(value).length;

/**
 * `strings.TrimSpace`, not `String.prototype.trim`.
 *
 * The two sets disagree at both ends — Go trims U+0085 and JS does not, JS
 * trims U+FEFF and Go does not — which is enough to make one side call a
 * filter empty while the other reads it as a value.
 */
const GO_SPACE =
  /^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g;
const goTrimSpace = (value) => value.replace(GO_SPACE, "");

/** Query validation mirrors the canonical LiveReportQuery contract. */
export function validateLiveQuery(type, data) {
  const errors = [];
  const fields = {
    "live-initiatives": [
      "board_refs",
      "project_ref",
      "card_ref",
      "limit",
      "sort",
    ],
    "live-cards": [
      "board_refs",
      "project_ref",
      "card_ref",
      "label",
      "role",
      "status",
      "limit",
      "sort",
    ],
    "live-work-mix": ["board_refs", "project_ref", "card_ref", "group_by"],
    "live-asks": [
      "limit",
      "include_answered",
      "answered_only",
      "answered_within_hours",
      "card_ref",
    ],
    "live-activity": ["limit"],
    "live-fleet-health": [],
  }[type];
  if (!fields || !data || typeof data !== "object" || Array.isArray(data))
    return ["must be a live query object"];
  if (Object.keys(data).some((key) => !fields.includes(key)))
    errors.push("contains unsupported query fields");
  if (
    data.board_refs !== undefined &&
    (!Array.isArray(data.board_refs) ||
      data.board_refs.length > 16 ||
      new Set(data.board_refs).size !== data.board_refs.length ||
      data.board_refs.some(
        (ref) =>
          typeof ref !== "string" ||
          !/^board:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(ref),
      ))
  )
    errors.push("board_refs must contain at most 16 unique board refs");
  if (
    data.project_ref !== undefined &&
    (typeof data.project_ref !== "string" ||
      !/^topic:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(data.project_ref))
  )
    errors.push("project_ref must be a topic ref");
  if (
    data.card_ref !== undefined &&
    (typeof data.card_ref !== "string" ||
      !/^card:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(data.card_ref))
  )
    errors.push("card_ref must be a card ref");
  if (
    data.limit !== undefined &&
    (!Number.isInteger(data.limit) || data.limit < 1 || data.limit > 100)
  )
    errors.push("limit must be an integer from 1 to 100");
  if (
    data.sort !== undefined &&
    !["priority", "updated", "title"].includes(data.sort)
  )
    errors.push("sort must be priority, updated, or title");
  if (
    data.group_by !== undefined &&
    !["phase", "board"].includes(data.group_by)
  )
    errors.push("group_by must be phase or board");
  // Exact-match card filters: a nonempty string of at most 128 bytes.
  //
  // Bytes, and Go's whitespace set, because core measures both that way. A
  // label of seventy accented characters is 140 bytes: counting UTF-16 units
  // here would call it valid and leave the write gate to reject it.
  for (const key of ["label", "role", "status"])
    if (
      data[key] !== undefined &&
      (typeof data[key] !== "string" ||
        !goTrimSpace(data[key]) ||
        utf8Length(data[key]) > 128)
    )
      errors.push("invalid card filter");
  if (
    data.include_answered !== undefined &&
    typeof data.include_answered !== "boolean"
  )
    errors.push("include_answered must be a boolean");
  if (
    data.answered_only !== undefined &&
    typeof data.answered_only !== "boolean"
  )
    errors.push("answered_only must be a boolean");
  if (
    data.answered_within_hours !== undefined &&
    (!Number.isInteger(data.answered_within_hours) ||
      data.answered_within_hours < 1 ||
      data.answered_within_hours > 720)
  )
    errors.push("answered_within_hours must be an integer from 1 to 720");
  return errors;
}

/** Presentation freshness must reflect the actual live read, never authored fields. */
export function withLiveObservation(panel, observation) {
  if (panel.source) return withSeriesObservation(panel, observation);
  if (!isLivePanel(panel)) return panel;
  return {
    ...panel,
    observed_at: observation?.observed_at ?? null,
    freshness: observation?.status === "ok" ? "current" : "unavailable",
    live: observation ?? { status: "loading", data: {} },
  };
}

export function formatLiveAge(seconds, now = Date.now()) {
  return formatAgeSeconds(seconds, now);
}
