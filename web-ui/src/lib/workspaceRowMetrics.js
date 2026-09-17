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
