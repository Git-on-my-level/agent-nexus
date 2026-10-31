import { withSeriesObservation } from "./seriesReports.js";
import { formatAgeSeconds } from "./time/format.js";

export const LIVE_REPORT_TYPES = Object.freeze([
  "live-collection",
  "live-loops",
  "live-loop",
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

/** State keys each collection accepts in a `live-collection` status filter. */
export const COLLECTION_STATES = Object.freeze({
  initiatives: ["waiting_on_you", "at_risk", "stalled", "on_track"],
  loops: [
    "failing",
    "stopped_reporting",
    "flat",
    "improving",
    "on_schedule",
    "unknown",
  ],
  agents: ["stuck", "offline", "idle", "working"],
});

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
    "live-loops": ["board", "health", "limit"],
    "live-loop": ["card"],
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
    "live-collection": [
      "collection",
      "project_ref",
      "status",
      "group_by",
      "limit",
    ],
    "live-activity": ["limit", "min_impact", "subject_refs", "collection"],
    "live-fleet-health": [],
  }[type];
  if (!fields || !data || typeof data !== "object" || Array.isArray(data))
    return ["must be a live query object"];
  if (type === "live-loops") {
    if (
      data.board !== undefined &&
      (typeof data.board !== "string" ||
        !/^board:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(data.board))
    )
      errors.push("invalid loop board");
    if (
      data.health !== undefined &&
      (typeof data.health !== "string" ||
        data.health
          .split(",")
          .some(
            (h) =>
              !["healthy", "stalled", "failing", "overdue", "unknown"].includes(
                h,
              ),
          ))
    )
      errors.push("invalid loop health");
    if (data.limit !== undefined && data.limit > 48)
      errors.push("loop limit exceeds 48");
  }
  if (
    type === "live-loop" &&
    (typeof data.card !== "string" ||
      !/^card:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(data.card))
  )
    errors.push("card is required");
  if (type === "live-collection") {
    // Mirrors contracts/visualreport/query.go: one of three collections,
    // its own state keys, project or no grouping, at most 48 rows.
    const states = COLLECTION_STATES[data.collection];
    if (!states) errors.push("invalid collection query");
    if (
      data.status !== undefined &&
      (!Array.isArray(data.status) ||
        data.status.length > 8 ||
        new Set(data.status).size !== data.status.length ||
        data.status.some((key) => !states?.includes(key)))
    )
      errors.push("invalid or duplicate collection state");
    if (
      data.group_by !== undefined &&
      !["project", "none"].includes(data.group_by)
    )
      errors.push("group_by must be project or none");
    if (data.limit !== undefined && data.limit > 48)
      errors.push("collection limit exceeds 48");
  }
  if (type === "live-activity") {
    if (
      data.collection !== undefined &&
      !["", "loops", "initiatives"].includes(data.collection)
    )
      errors.push("invalid activity collection");
    if (
      data.subject_refs !== undefined &&
      (!Array.isArray(data.subject_refs) ||
        data.subject_refs.length > 100 ||
        new Set(data.subject_refs).size !== data.subject_refs.length ||
        data.subject_refs.some(
          (ref) =>
            typeof ref !== "string" ||
            utf8Length(ref) > 2048 ||
            !ref.includes(":") ||
            goTrimSpace(ref) !== ref,
        ))
    )
      errors.push("invalid or duplicate subject ref");
  }
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
    type !== "live-collection" &&
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
      !(type === "live-collection" && key === "status") &&
      data[key] !== undefined &&
      (typeof data[key] !== "string" ||
        !goTrimSpace(data[key]) ||
        utf8Length(data[key]) > 128)
    )
      errors.push("invalid card filter");
  if (
    data.min_impact !== undefined &&
    !["high", "normal", "low"].includes(data.min_impact)
  )
    errors.push("min_impact must be high, normal or low");
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

/** How long a live report read may take before the panel says so. */
export const LIVE_READ_TIMEOUT_MS = 20_000;

/**
 * Run `start(signal)` and settle within `ms`, whatever the transport does.
 *
 * A render read with no deadline that never answers left every live panel on
 * "Reading workspace…" for good: the poll skips while a read is in flight, so
 * one stalled request blocked every later one. This rejects with a
 * `TimeoutError` at the deadline and aborts the request, and an abort of the
 * outer `signal` (the report closing) cancels it too.
 *
 * @template T
 * @param {(signal: AbortSignal) => Promise<T>} start
 * @param {{ ms?: number, signal?: AbortSignal }} [options]
 * @returns {Promise<T>}
 */
export function readWithDeadline(
  start,
  { ms = LIVE_READ_TIMEOUT_MS, signal } = {},
) {
  const controller = new AbortController();
  const forward = () => controller.abort(signal?.reason);
  if (signal?.aborted) forward();
  else signal?.addEventListener("abort", forward, { once: true });
  let timer = 0;
  const deadline = new Promise((_, reject) => {
    const fail = () => reject(controller.signal.reason);
    if (controller.signal.aborted) fail();
    else controller.signal.addEventListener("abort", fail, { once: true });
    timer = setTimeout(() => {
      const error = new Error("The live read timed out.");
      error.name = "TimeoutError";
      controller.abort(error);
    }, ms);
  });
  return Promise.race([
    Promise.resolve().then(() => start(controller.signal)),
    deadline,
  ]).finally(() => {
    clearTimeout(timer);
    signal?.removeEventListener("abort", forward);
  });
}
