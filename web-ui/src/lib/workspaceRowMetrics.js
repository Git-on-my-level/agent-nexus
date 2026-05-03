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
 * @param {object | null | undefined} topic
 * @returns {{ key: string, count: number, label: string, dotClass: string }[]}
 */
export function topicListLinkedMetricItems(topic) {
  const timeline = Number(topic?.timeline_message_count ?? 0);
  const docRefs = Array.isArray(topic?.document_refs)
    ? topic.document_refs
    : [];
  const boardRefs = Array.isArray(topic?.board_refs) ? topic.board_refs : [];

  return [
    {
      key: "timeline_messages",
      count: timeline,
      label: "Messages",
      dotClass: "bg-accent",
    },
    {
      key: "documents",
      count: docRefs.length,
      label: "Documents",
      dotClass: "bg-blue-400",
    },
    {
      key: "boards",
      count: boardRefs.length,
      label: "Boards",
      dotClass: "bg-warn",
    },
  ];
}

/**
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

  const charsRaw = doc?.head_revision_character_count;
  const hasChars = typeof charsRaw === "number" && Number.isFinite(charsRaw);

  const characterChip = hasChars
    ? {
        key: "head_characters",
        label: "Characters",
        dotClass: "bg-fg-subtle",
        count: charsRaw,
      }
    : {
        key: "head_characters",
        label: "Characters",
        dotClass: "bg-fg-subtle",
        displayValue: "—",
      };

  return [
    {
      key: "timeline_messages",
      count: messages,
      label: "Messages",
      dotClass: "bg-accent",
    },
    {
      key: "revision_lineage",
      count: revisions,
      label: "Revisions",
      dotClass: "bg-blue-400",
    },
    characterChip,
  ];
}
