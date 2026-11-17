import { BOARD_COLUMN_KEY_VALUES } from "../../../contracts/gen/ts/dist/enums.js";
import { isInternalUuid, resourceRouteSegment } from "./resourceIdentity.js";

/** Lifecycle `state` on boards/topics/threads (API v0.4+). */
export const BOARD_LIFECYCLE_STATE_LABELS = {
  active: "Active",
  archived: "Archived",
  trashed: "Trashed",
};

/**
 * The board name as a list shows it: without the word the column header
 * already said.
 *
 * Boards are routinely named "Launch Readiness Board", so a BOARD column
 * reads "…Board" down every row and a card's board chip truncates on the
 * suffix ("Creative / Content Bo…"). Display only — the board keeps its
 * title, and a board actually called "Board" keeps it too.
 *
 * @param {unknown} title
 */
export function boardDisplayTitle(title) {
  const text = String(title ?? "").trim();
  const trimmed = text.replace(/\s+board$/i, "").trim();
  return trimmed || text;
}

/**
 * Board columns in contract order, titled from the key ("in_progress" reads
 * "In Progress"), so a column added to the schema renders without a second list.
 */
export const CANONICAL_BOARD_COLUMNS = BOARD_COLUMN_KEY_VALUES.map((key) => ({
  key,
  title: key.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()),
}));

export const CANONICAL_BOARD_COLUMN_KEYS = CANONICAL_BOARD_COLUMNS.map(
  (column) => column.key,
);

/** Preview row count for board workspace side panels (docs, resolved cards, inbox). */
export const BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT = 6;

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
