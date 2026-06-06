import { isInternalUuid, resourceRouteSegment } from "./resourceIdentity.js";

/** Lifecycle `state` on boards/topics/threads (API v0.4+). */
export const BOARD_LIFECYCLE_STATE_LABELS = {
  active: "Active",
  archived: "Archived",
  trashed: "Trashed",
};

/** @deprecated Use BOARD_LIFECYCLE_STATE_LABELS */
export const BOARD_STATUS_LABELS = BOARD_LIFECYCLE_STATE_LABELS;

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

/** Preview row count for board workspace side panels (docs, resolved cards, inbox). */
export const BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT = 6;

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
 * Stable key for API calls and UI state (public card handle/ref value, else legacy thread-backed id).
 * Falls back to a synthetic key when both are missing (corrupt/partial payload).
 */
export function boardCardStableId(membership) {
  const publicSegment = resourceRouteSegment(membership, "card");
  if (publicSegment) return publicSegment;
  const id = String(membership?.id ?? "").trim();
  if (id && !isInternalUuid(id)) return id;
  const legacy = String(membership?.thread_id ?? "").trim();
  if (legacy) return legacy;
  const col = String(membership?.column_key ?? "").trim();
  const rank = String(membership?.rank ?? "").trim();
  const created = String(membership?.created_at ?? "").trim();
  const parts = [col, rank, created].filter(Boolean).join(":");
  if (parts) return `anon:${parts}`;
  return "anon:board-card";
}

/**
 * Canonical card row id for `before_card_id` / `after_card_id` on cards.move.
 * Differs from {@link boardCardStableId}: handles and thread ids are not valid anchors.
 */
export function boardCardPlacementAnchorId(membership) {
  return String(membership?.id ?? "").trim();
}

/** Card row title: membership title, else backing thread title, else stable id. */
export function boardCardHeaderTitle(membership, thread) {
  const cardTitle = String(membership?.title ?? "").trim();
  if (cardTitle) return cardTitle;
  const threadTitle = String(thread?.title ?? "").trim();
  if (threadTitle) return threadTitle;
  return boardCardStableId(membership);
}

/**
 * `message_posted` count for a board workspace card row (`/boards/{id}/workspace`).
 * Core exposes `derived.timeline_message_count`. Missing or invalid values → 0.
 */
export function boardCardTimelineMessageCount(cardItem) {
  const d = cardItem?.derived;
  const raw =
    d?.timeline_message_count ??
    d?.timelineMessageCount ??
    cardItem?.timeline_message_count;
  const n = Math.floor(Number(raw ?? 0));
  return Number.isFinite(n) ? Math.max(0, n) : 0;
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

/**
 * Stable ids for cards in a column, ordered by ascending `membership.rank` (numeric).
 *
 * @param {{ items?: unknown[] } | null | undefined} cardsSection
 * @param {unknown[] | null | undefined} columnSchema
 * @param {string | null | undefined} columnKey
 * @returns {string[]}
 */
export function sortedColumnPeersStableIds(
  cardsSection,
  columnSchema,
  columnKey,
) {
  const key = String(columnKey ?? "").trim();
  if (!key) return [];

  const grouped = groupBoardWorkspaceCards(cardsSection, columnSchema);
  return [...(grouped[key] ?? [])]
    .sort((left, right) => {
      const a = Number.parseInt(String(left?.membership?.rank ?? "0"), 10);
      const b = Number.parseInt(String(right?.membership?.rank ?? "0"), 10);
      const safeA = Number.isFinite(a) ? a : 0;
      const safeB = Number.isFinite(b) ? b : 0;
      if (safeA !== safeB) return safeA - safeB;
      return String(left?.membership?.created_at ?? "").localeCompare(
        String(right?.membership?.created_at ?? ""),
      );
    })
    .map((item) => boardCardStableId(item?.membership))
    .filter(Boolean);
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
      return "text-ok-text bg-ok-soft";
    case "pending":
      return "text-warn-text bg-warn-soft";
    case "error":
      return "text-danger-text bg-danger-soft";
    case "missing":
      return "text-fg-muted bg-bg-soft";
    default:
      return "text-fg-muted bg-line";
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
      return "text-blue-400 bg-blue-400/10";
    case "in_progress":
      return "text-warn-text bg-warn-soft";
    case "blocked":
      return "text-danger-text bg-danger-soft";
    case "review":
      return "text-accent-text bg-accent-soft";
    case "done":
      return "text-ok-text bg-ok-soft";
    case "canceled":
    case "cancelled":
      return "text-fg-muted bg-line";
    case "paused":
      return "text-warn-text bg-warn-soft";
    default:
      return "text-fg-muted bg-line";
  }
}
