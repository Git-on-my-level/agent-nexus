import { boardColumnTitle, CANONICAL_BOARD_COLUMN_KEYS } from "./boardUtils.js";

/** Dot colors for canonical board columns in dense list metric strips (matches column semantics). */
export const BOARD_COLUMN_LIST_DOT_CLASSES = Object.freeze({
  backlog: "bg-fg-subtle",
  ready: "bg-blue-400",
  in_progress: "bg-warn",
  blocked: "bg-danger",
  review: "bg-accent",
  done: "bg-ok",
});

/**
 * @param {object | null | undefined} board
 * @param {object | null | undefined} listStats
 * @returns {{ key: string, count: number, label: string, dotClass: string }[]}
 */
export function boardListColumnMetricItems(board, listStats) {
  const cols = listStats?.cards_by_column ?? {};
  const schema = Array.isArray(board?.column_schema) ? board.column_schema : [];
  const fromSchema = schema
    .map((column) => String(column?.key ?? "").trim())
    .filter(Boolean);
  const schemaSet = new Set(fromSchema);
  const canonicalFallback = [...CANONICAL_BOARD_COLUMN_KEYS];
  /** @type {string[]} */
  let keyOrder = fromSchema.length ? fromSchema : canonicalFallback;

  const extraKeys = Object.keys(cols).filter((k) => !schemaSet.has(k));
  extraKeys.sort();
  if (extraKeys.length) {
    keyOrder = [...new Set([...keyOrder, ...extraKeys])];
  }

  return keyOrder.map((key) => ({
    key,
    count: Number(cols[key] ?? 0),
    label: boardColumnTitle(key, schema),
    dotClass: BOARD_COLUMN_LIST_DOT_CLASSES[key] ?? "bg-fg-subtle",
  }));
}

/**
 * Row metrics for a document.
 *
 * The character count is gone: nobody decides anything from "1,482
 * characters", and it sat next to two numbers that do carry meaning. The dots
 * are all one neutral colour — they separate chips, they do not encode a
 * status — and the labels use the words the product uses everywhere else
 * (Comments, Versions), not the storage layer's (Messages, Revisions).
 *
 * @param {object | null | undefined} doc
 * @returns {Array<{ key: string, label: string, dotClass: string, count?: number, displayValue?: string }>}
 */
export function documentListMetricItems(doc) {
  const messages = Number(doc?.timeline_message_count ?? 0);

  let revisions;
  const rc = doc?.revision_count;
  if (typeof rc === "number" && Number.isFinite(rc)) {
    revisions = rc;
  } else {
    revisions = Number(doc?.head_revision_number ?? 0);
  }

  return [
    {
      key: "timeline_messages",
      count: messages,
      label: "Comments",
      dotClass: "bg-line-strong",
    },
    {
      key: "revision_lineage",
      count: revisions,
      label: "Versions",
      dotClass: "bg-line-strong",
    },
  ];
}
