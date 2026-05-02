/** @param {unknown} row @param {string} key */
export function formatRevisionFieldPlain(row, key) {
  if (!row || typeof row !== "object") return "";
  const v = /** @type {Record<string, unknown>} */ (row)[key];
  if (Array.isArray(v)) {
    return v
      .map((x) => String(x).trim())
      .filter(Boolean)
      .join("\n");
  }
  if (v == null) return "";
  return String(v).trim();
}

export const CARD_REVISION_DIFF_FIELD_KEYS = [
  "title",
  "summary",
  "definition_of_done",
  "assignee_refs",
  "related_refs",
  "refs",
  "resolution_refs",
  "column_key",
  "resolution",
  "risk",
  "due_at",
];

/** @param {string} before @param {string} after @param {number} maxLines */
export function summarizeLineTextDelta(before, after, maxLines = 8) {
  const a = String(before ?? "")
    .replace(/\r\n/g, "\n")
    .split("\n");
  const b = String(after ?? "")
    .replace(/\r\n/g, "\n")
    .split("\n");
  /** @type {{ kind: "add" | "remove"; text: string }[]} */
  const out = [];
  const n = Math.max(a.length, b.length);
  for (let i = 0; i < n && out.length < maxLines; i++) {
    const al = a[i] ?? "";
    const bl = b[i] ?? "";
    if (al === bl) continue;
    if (al) out.push({ kind: "remove", text: al });
    if (bl) out.push({ kind: "add", text: bl });
  }
  if (out.length === 0 && before !== after) {
    return [
      { kind: "remove", text: clipOneLine(before, 140) },
      { kind: "add", text: clipOneLine(after, 140) },
    ];
  }
  return out;
}

/** @param {unknown} s @param {number} max */
function clipOneLine(s, max) {
  const t = String(s ?? "")
    .trim()
    .replace(/\s+/g, " ");
  if (t.length <= max) return t;
  return `${t.slice(0, max - 1)}…`;
}

export function humanizeRevisionFieldKey(key) {
  return String(key ?? "")
    .replace(/_/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

/**
 * @param {Record<string, unknown> | null | undefined} parent older revision (larger revision_number in history order when sorted desc means parent is next item)
 * @param {Record<string, unknown>} rev
 * @returns {{ field: string; lines: { kind: string; text: string }[] }[]}
 */
export function diffCardRevisionAgainstParent(parent, rev) {
  if (!rev || typeof rev !== "object") return [];
  /** @type {{ field: string; lines: { kind: string; text: string }[] }[]} */
  const out = [];
  for (const field of CARD_REVISION_DIFF_FIELD_KEYS) {
    const b = formatRevisionFieldPlain(parent, field);
    const a = formatRevisionFieldPlain(rev, field);
    if (b === a) continue;
    if (field === "summary") {
      out.push({
        field,
        lines: summarizeLineTextDelta(b, a, 10),
      });
      continue;
    }
    if (
      field === "definition_of_done" ||
      field === "assignee_refs" ||
      field === "related_refs" ||
      field === "refs" ||
      field === "resolution_refs"
    ) {
      const lines = summarizeLineTextDelta(b, a, 8);
      out.push({ field, lines });
      continue;
    }
    out.push({
      field,
      lines: [
        { kind: "remove", text: clipOneLine(b || "—", 120) },
        { kind: "add", text: clipOneLine(a || "—", 120) },
      ],
    });
  }
  return out;
}
