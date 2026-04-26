/**
 * Utilities for wrapping the quoted range of a document text comment with one
 * or more `<mark>` elements inside the rendered markdown body, so the operator
 * can see at a glance which span their pending comment is anchored to (Google
 * Docs–style highlight on the selection while you compose).
 *
 * Implementation notes:
 *   - The markdown is rendered as opaque HTML by `MarkdownRenderer`, so we
 *     can't reuse the offsets from the raw revision string. We work over
 *     the rendered DOM's text content instead.
 *   - We create *one `<mark>` per text node fragment* that the quote covers.
 *     Each mark shares the same `data-event-id` and `data-doc-comment-mark`
 *     attributes. This is critical for multi-line / multi-block quotes:
 *     the older approach of wrapping the entire `Range` in a single inline
 *     `<mark>` would either throw on cross-element ranges or fall back to
 *     putting block elements inside an inline element, which left some lines
 *     visually un-highlighted. Per-text-node marks always render correctly.
 *   - Code blocks and pre-formatted regions are skipped: highlighting in
 *     them is rarely useful and risks breaking syntax styling.
 *   - The first occurrence wins (matches `documentCommentAnchor.js`'s
 *     "unique single match" semantics; ambiguous selections won't get a
 *     visual highlight, which is the right behavior — the chip on the
 *     comment card will say "Quote only" instead).
 *
 * The marks are tagged with a class and `data-doc-comment-mark="1"` so a
 * caller can clear them on the next pass via `clearDocumentCommentMarks`.
 */

const MARK_CLASS = "js-doc-comment-mark";
const MARK_ATTR = "data-doc-comment-mark";
const SKIP_TAGS = new Set(["CODE", "PRE", "SCRIPT", "STYLE"]);

/**
 * Remove every `<mark>` previously inserted by `highlightDocumentCommentRange`,
 * unwrapping its child nodes back into the parent so the original DOM text
 * shape is restored. Safe to call when no marks exist.
 *
 * @param {HTMLElement | null | undefined} root
 */
export function clearDocumentCommentMarks(root) {
  if (!root || typeof root.querySelectorAll !== "function") {
    return;
  }
  const marks = root.querySelectorAll(`mark[${MARK_ATTR}]`);
  for (const mark of Array.from(marks)) {
    const parent = mark.parentNode;
    if (!parent) continue;
    while (mark.firstChild) {
      parent.insertBefore(mark.firstChild, mark);
    }
    parent.removeChild(mark);
    if (typeof parent.normalize === "function") {
      parent.normalize();
    }
  }
}

/**
 * Walk text nodes inside `root` in document order, returning the flat list
 * of `(textNode, aggregateStart, aggregateEnd)` tuples and the joined
 * aggregate string. Skips text inside `<code>`, `<pre>`, `<script>`,
 * `<style>`, and inside any element already tagged as a doc comment mark.
 *
 * @param {HTMLElement} root
 */
function collectTextSegments(root) {
  /** @type {Array<{ node: Text, start: number, end: number }>} */
  const segments = [];
  let aggregate = "";

  /** @param {Node} node */
  function visit(node) {
    if (node.nodeType === 1) {
      const el = /** @type {Element} */ (node);
      if (SKIP_TAGS.has(el.tagName)) return;
      if (el.hasAttribute && el.hasAttribute(MARK_ATTR)) return;
      for (let i = 0; i < el.childNodes.length; i++) {
        visit(el.childNodes[i]);
      }
      return;
    }
    if (node.nodeType === 3) {
      const text = /** @type {Text} */ (node);
      const value = text.data || "";
      if (value.length === 0) return;
      const start = aggregate.length;
      aggregate += value;
      segments.push({ node: text, start, end: start + value.length });
    }
  }

  visit(root);
  return { segments, aggregate };
}

/**
 * Build the inline style string for a doc-comment `<mark>` based on tone.
 *
 * Tones:
 *   - pending: solid soft accent fill (you're composing right now)
 *   - posted:  subtle dashed underline (an existing comment lives here)
 *
 * @param {"pending" | "posted"} tone
 */
function styleForTone(tone) {
  if (tone === "posted") {
    return {
      backgroundColor: "color-mix(in oklab, var(--accent) 8%, transparent)",
      borderBottom: "1px dashed var(--accent)",
      color: "inherit",
      borderRadius: "2px",
      padding: "0 1px",
      cursor: "pointer",
    };
  }
  return {
    backgroundColor: "color-mix(in oklab, var(--accent) 22%, transparent)",
    color: "inherit",
    borderRadius: "2px",
    padding: "0 1px",
  };
}

/**
 * Wrap one contiguous slice of a single text node in a `<mark>` element.
 * Returns the new mark, or `null` if the slice was empty. Splits the text
 * node into up-to-three pieces (before/marked/after) so adjacent
 * highlights survive without clobbering each other.
 *
 * @param {Document} doc
 * @param {Text} textNode
 * @param {number} startInNode
 * @param {number} endInNode
 * @param {{ tone: "pending" | "posted", eventId?: string }} opts
 */
function wrapTextNodeSlice(doc, textNode, startInNode, endInNode, opts) {
  if (endInNode <= startInNode) return null;
  const len = (textNode.data || "").length;
  const s = Math.max(0, Math.min(len, startInNode));
  const e = Math.max(s, Math.min(len, endInNode));
  if (e <= s) return null;
  const range = doc.createRange();
  try {
    range.setStart(textNode, s);
    range.setEnd(textNode, e);
  } catch {
    return null;
  }
  const mark = doc.createElement("mark");
  mark.className = `${MARK_CLASS} ${
    opts.tone === "posted" ? "is-posted" : "is-pending"
  }`;
  mark.setAttribute(MARK_ATTR, "1");
  if (opts.eventId) {
    mark.setAttribute("data-event-id", String(opts.eventId));
  }
  const style = styleForTone(opts.tone);
  for (const [k, v] of Object.entries(style)) {
    /** @type {any} */ (mark.style)[k] = v;
  }
  try {
    range.surroundContents(mark);
    return mark;
  } catch {
    return null;
  }
}

/**
 * Wrap the first occurrence of `quote` inside `root` with one `<mark>` per
 * text node fragment that the quote covers. Replaces existing doc-comment
 * marks first unless `opts.clear === false`. Returns `true` if any mark was
 * created.
 *
 * @param {HTMLElement | null | undefined} root
 * @param {string} quote
 * @param {{ tone?: "pending" | "posted", eventId?: string, clear?: boolean }} [opts]
 *   - `clear` (default `true`): clear existing marks first. Set `false` when
 *     applying multiple layers (see `applyDocumentCommentHighlights`).
 */
export function highlightDocumentCommentRange(root, quote, opts = {}) {
  if (opts.clear !== false) {
    clearDocumentCommentMarks(root);
  }
  if (!root) return false;
  const text = String(quote ?? "");
  if (!text || !text.trim()) return false;
  const doc = root.ownerDocument;
  if (!doc) return false;

  const { segments, aggregate } = collectTextSegments(root);
  if (segments.length === 0) return false;
  const idx = aggregate.indexOf(text);
  if (idx < 0) return false;
  const endIdx = idx + text.length;

  const tone = opts.tone === "posted" ? "posted" : "pending";
  const wrapOpts = {
    tone,
    eventId: opts.eventId ? String(opts.eventId) : "",
  };

  // Walk segments and wrap each overlapping slice in its own mark. We capture
  // the text nodes first so splitting (via surroundContents) doesn't corrupt
  // the live segment list.
  /** @type {Array<{ node: Text, start: number, end: number }>} */
  const overlaps = [];
  for (const seg of segments) {
    if (seg.end <= idx) continue;
    if (seg.start >= endIdx) break;
    const startInNode = Math.max(0, idx - seg.start);
    const endInNode = Math.min(seg.end - seg.start, endIdx - seg.start);
    if (endInNode > startInNode) {
      overlaps.push({ node: seg.node, start: startInNode, end: endInNode });
    }
  }

  let any = false;
  for (const piece of overlaps) {
    const mark = wrapTextNodeSlice(
      doc,
      piece.node,
      piece.start,
      piece.end,
      wrapOpts,
    );
    if (mark) any = true;
  }
  return any;
}

/**
 * Clear once, then highlight every posted (non-pending) quote, then a pending
 * quote if present. When the same quoted text is both a posted comment and
 * the active pending selection, only the pending mark is shown.
 *
 * @param {HTMLElement | null | undefined} root
 * @param {{ posted?: Array<{ quote: string, eventId?: string }>, pendingQuote?: string }} [options]
 */
export function applyDocumentCommentHighlights(root, options = {}) {
  const posted = Array.isArray(options.posted) ? options.posted : [];
  const pendingRaw = String(options.pendingQuote ?? "");
  const pendingTrim = pendingRaw.trim();
  clearDocumentCommentMarks(root);
  if (!root) return;
  for (const row of posted) {
    const q = String(row?.quote ?? "").trim();
    if (!q) continue;
    if (pendingTrim && q === pendingTrim) {
      continue;
    }
    highlightDocumentCommentRange(root, q, {
      tone: "posted",
      eventId: row?.eventId ? String(row.eventId) : "",
      clear: false,
    });
  }
  if (pendingTrim) {
    highlightDocumentCommentRange(root, pendingTrim, {
      tone: "pending",
      clear: false,
    });
  }
}
