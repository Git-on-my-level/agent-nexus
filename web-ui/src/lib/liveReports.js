export const LIVE_REPORT_TYPES = Object.freeze([
  "live-initiatives",
  "live-asks",
  "live-work-mix",
  "live-activity",
]);
export const isLivePanel = (panel) => LIVE_REPORT_TYPES.includes(panel?.type);

/** Query validation mirrors the canonical LiveReportQuery contract. */
export function validateLiveQuery(type, data) {
  const errors = [];
  const fields = {
    "live-initiatives": ["board_refs", "project_ref", "limit", "sort"],
    "live-work-mix": ["board_refs", "project_ref", "group_by"],
    "live-asks": ["limit", "include_answered", "answered_within_hours"],
    "live-activity": ["limit"],
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
  if (
    data.include_answered !== undefined &&
    typeof data.include_answered !== "boolean"
  )
    errors.push("include_answered must be a boolean");
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
  if (!isLivePanel(panel)) return panel;
  return {
    ...panel,
    observed_at: observation?.observed_at ?? null,
    freshness: observation?.status === "ok" ? "current" : "unavailable",
    live: observation ?? { status: "loading", data: {} },
  };
}

export function formatLiveAge(seconds) {
  if (!Number.isFinite(seconds)) return "Age unknown";
  if (seconds < 60) return "Just now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m old`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h old`;
  return `${Math.floor(seconds / 86400)}d old`;
}
