/** @type {string} */
export const RECENT_ASSIGNEE_IDS_KEY = "anx.recent-assignee-ids";

const MAX_IDS = 24;

/**
 * Normalize assignee ref to bare actor id for storage and ordering.
 * @param {unknown} raw
 * @returns {string}
 */
export function normalizeAssigneeIdForRecency(raw) {
  let s = String(raw ?? "").trim();
  if (!s) return "";
  if (s.includes(":")) {
    const idx = s.indexOf(":");
    const prefix = s.slice(0, idx).toLowerCase();
    if (prefix === "actor") {
      s = s.slice(idx + 1).trim();
    }
  }
  return s;
}

/**
 * @returns {string[]}
 */
export function readRecentAssigneeIds() {
  if (typeof localStorage === "undefined") return [];
  try {
    const raw = localStorage.getItem(RECENT_ASSIGNEE_IDS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return [
      ...new Set(
        parsed.map((x) => normalizeAssigneeIdForRecency(x)).filter(Boolean),
      ),
    ];
  } catch {
    return [];
  }
}

/**
 * Touch recency for assignees (most recently used first).
 * @param {unknown[]} ids
 */
export function touchRecentAssigneeIds(ids) {
  if (typeof localStorage === "undefined") return;
  const incoming = (ids ?? [])
    .map((x) => normalizeAssigneeIdForRecency(x))
    .filter(Boolean);
  if (!incoming.length) return;
  const merged = [...incoming, ...readRecentAssigneeIds()].filter(Boolean);
  const next = [...new Set(merged)].slice(0, MAX_IDS);
  try {
    localStorage.setItem(RECENT_ASSIGNEE_IDS_KEY, JSON.stringify(next));
  } catch {
    /* ignore quota / private mode */
  }
}

/**
 * Stable-sort picker options so recently used ids appear first.
 * @param {{ id: string }[]} options
 * @param {string[]} recentIds most recent first
 * @returns {{ id: string }[]}
 */
export function orderPickerOptionsByRecent(options, recentIds) {
  const rank = new Map(
    (recentIds ?? []).map((id, i) => [normalizeAssigneeIdForRecency(id), i]),
  );
  return [...(options ?? [])]
    .map((opt, orig) => ({
      opt,
      orig,
      r: rank.has(opt.id) ? /** @type {number} */ (rank.get(opt.id)) : Infinity,
    }))
    .sort((a, b) => a.r - b.r || a.orig - b.orig)
    .map((d) => d.opt);
}
