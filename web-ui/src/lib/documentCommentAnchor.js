/** Context window (UTF-16 code units) for before/after snippets in the source. */
const CONTEXT_LEN = 48;

/**
 * @param {string} s
 */
function normalizeLineEndings(s) {
  return String(s ?? "").replaceAll("\r\n", "\n");
}

/**
 * @param {string} source
 * @param {number} start
 * @param {number} end
 */
function contextAround(source, start, end) {
  const src = String(source ?? "");
  const a = Math.max(0, start - CONTEXT_LEN);
  const b = Math.min(src.length, end + CONTEXT_LEN);
  return {
    context_before: src.slice(a, start),
    context_after: src.slice(end, b),
  };
}

/**
 * Build anchor metadata for a document text comment. Maps selected text to
 * offsets in the raw revision `source` when the selection matches exactly once.
 *
 * @param {object} opts
 * @param {string} opts.source Raw markdown / revision content
 * @param {string} opts.selectedText Browser selection (may need trimming)
 * @param {string} opts.documentId
 * @param {string} opts.revisionId Active revision for this view
 * @param {string} [opts.contentHash]
 * @param {boolean} [opts.isHeadRevision] When false, `anchor_status` is `historical` for mapped anchors
 * @returns {object} Normalized `document_comment` object for a `message_posted` payload
 */
export function buildDocumentCommentFields({
  source,
  selectedText,
  documentId,
  revisionId,
  contentHash = "",
  isHeadRevision = true,
} = {}) {
  const src = normalizeLineEndings(String(source ?? ""));
  const rawSel = normalizeLineEndings(String(selectedText ?? ""));
  const sel = rawSel.trim();
  const docId = String(documentId ?? "").trim();
  const revId = String(revisionId ?? "").trim();
  const hash = String(contentHash ?? "").trim();

  const base = {
    document_id: docId,
    revision_id: revId,
    content_hash: hash,
    selected_text: sel,
    context_before: "",
    context_after: "",
    start_offset: null,
    end_offset: null,
    anchor_status: "quote_only",
  };

  if (!docId || !revId || !sel) {
    return base;
  }

  const matches = [];
  let from = 0;
  let idx;
  while ((idx = src.indexOf(sel, from)) >= 0) {
    matches.push([idx, idx + sel.length]);
    from = idx + 1;
  }

  if (matches.length !== 1) {
    if (sel.length > 0) {
      const head = sel.slice(0, Math.min(CONTEXT_LEN, sel.length));
      const tail = sel.slice(Math.max(0, sel.length - CONTEXT_LEN));
      base.context_before = head;
      base.context_after = tail;
    }
    return base;
  }

  const [start, end] = matches[0];
  const { context_before, context_after } = contextAround(src, start, end);
  base.context_before = context_before;
  base.context_after = context_after;
  base.start_offset = start;
  base.end_offset = end;
  base.selected_text = src.slice(start, end);
  base.anchor_status = isHeadRevision ? "current" : "historical";
  return base;
}
