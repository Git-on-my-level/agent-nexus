export const BOARD_STATUS_LABELS = {
  active: "Active",
  paused: "Paused",
  closed: "Closed",
};

export const CANONICAL_BOARD_COLUMNS = [
  { key: "backlog", title: "Backlog" },
  { key: "ready", title: "Ready" },
  { key: "in_progress", title: "In Progress" },
  { key: "blocked", title: "Blocked" },
  { key: "review", title: "Review" },
  { key: "done", title: "Done" },
];

export const CANONICAL_BOARD_COLUMN_KEYS = CANONICAL_BOARD_COLUMNS.map(
  (column) => column.key,
);

export function createEmptyBoardColumnCounts() {
  return CANONICAL_BOARD_COLUMNS.reduce((counts, column) => {
    counts[column.key] = 0;
    return counts;
  }, {});
}

export function boardColumnTitle(columnKey, columnSchema = []) {
  const configured = (columnSchema ?? []).find(
    (column) => column?.key === columnKey,
  );
  if (configured?.title) {
    return configured.title;
  }

  const canonical = CANONICAL_BOARD_COLUMNS.find(
    (column) => column.key === columnKey,
  );
  return canonical?.title ?? columnKey;
}

export function boardSummaryCounts(summary) {
  const counts = createEmptyBoardColumnCounts();

  for (const [columnKey, count] of Object.entries(
    summary?.cards_by_column ?? {},
  )) {
    counts[columnKey] = Number(count ?? 0);
  }

  return counts;
}

/** Board thread id for a board row (core `thread_id`, event timeline). */
export function boardBackingThreadId(board) {
  return String(board?.thread_id ?? "").trim();
}

/** First `document:` id from `document_refs` or `refs`. */
export function firstBoardDocumentId(board) {
  const fromList = (list) => {
    for (const ref of list ?? []) {
      const s = String(ref ?? "").trim();
      if (s.startsWith("document:")) {
        return s.slice("document:".length).trim();
      }
    }
    return "";
  };
  const doc = fromList(board?.document_refs);
  if (doc) return doc;
  return fromList(board?.refs);
}

export function boardCardLinkedThreadId(membership) {
  return String(membership?.thread_id ?? "").trim();
}

/**
 * Stable key for API calls and UI state (versioned card id, else legacy thread-backed id).
 * Falls back to a synthetic key when both are missing (corrupt/partial payload).
 */
export function boardCardStableId(membership) {
  const id = String(membership?.id ?? "").trim();
  if (id) return id;
  const legacy = String(membership?.thread_id ?? "").trim();
  if (legacy) return legacy;
  const col = String(membership?.column_key ?? "").trim();
  const rank = String(membership?.rank ?? "").trim();
  const created = String(membership?.created_at ?? "").trim();
  const parts = [col, rank, created].filter(Boolean).join(":");
  if (parts) return `anon:${parts}`;
  return "anon:board-card";
}

export function groupBoardWorkspaceCards(cardsSection, columnSchema = []) {
  const groups = (columnSchema?.length ? columnSchema : CANONICAL_BOARD_COLUMNS)
    .map((column) => column.key)
    .reduce((acc, columnKey) => {
      acc[columnKey] = [];
      return acc;
    }, {});

  for (const item of cardsSection?.items ?? []) {
    const columnKey = String(item?.membership?.column_key ?? "").trim();
    if (!groups[columnKey]) {
      groups[columnKey] = [];
    }
    groups[columnKey].push(item);
  }

  return groups;
}

export function parseDelimitedValues(rawValue) {
  const seen = new Set();
  const values = [];

  for (const item of String(rawValue ?? "").split(/\r?\n|,/)) {
    const value = item.trim();
    if (!value || seen.has(value)) {
      continue;
    }
    seen.add(value);
    values.push(value);
  }

  return values;
}

export function joinDelimitedValues(items) {
  return (items ?? [])
    .map((item) => String(item ?? "").trim())
    .filter(Boolean)
    .join("\n");
}

export function freshnessStatusLabel(status) {
  switch (String(status ?? "").trim()) {
    case "current":
      return "Current";
    case "pending":
      return "Pending refresh";
    case "error":
      return "Refresh error";
    case "missing":
      return "Not materialized";
    default:
      return "Unknown freshness";
  }
}

export function freshnessStatusTone(status) {
  switch (String(status ?? "").trim()) {
    case "current":
      return "text-emerald-300 bg-emerald-500/10";
    case "pending":
      return "text-amber-300 bg-amber-500/10";
    case "error":
      return "text-red-300 bg-red-500/10";
    case "missing":
      return "text-slate-300 bg-slate-500/10";
    default:
      return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }
}

export function isFreshnessCurrent(freshness) {
  return String(freshness?.status ?? "").trim() === "current";
}

export function cardStatusTagColor(status) {
  switch (
    String(status ?? "")
      .trim()
      .toLowerCase()
      .replace(/[\s-]+/g, "_")
  ) {
    case "todo":
      return "text-blue-400 bg-blue-500/10";
    case "in_progress":
      return "text-amber-300 bg-amber-500/10";
    case "blocked":
      return "text-red-400 bg-red-500/10";
    case "review":
      return "text-purple-400 bg-purple-500/10";
    case "done":
      return "text-emerald-400 bg-emerald-500/10";
    case "canceled":
    case "cancelled":
      return "text-gray-500 bg-gray-500/10";
    case "paused":
      return "text-amber-400 bg-amber-400/10";
    default:
      return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }
}

export function cardPriorityTagColor(priority) {
  switch (
    String(priority ?? "")
      .trim()
      .toLowerCase()
  ) {
    case "critical":
    case "urgent":
      return "text-red-400 bg-red-500/10";
    case "high":
      return "text-orange-400 bg-orange-500/10";
    case "medium":
      return "text-amber-300 bg-amber-500/10";
    case "low":
      return "text-blue-400 bg-blue-500/10";
    default:
      return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }
}
