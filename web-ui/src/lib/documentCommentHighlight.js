/**
 * Utilities for wrapping the quoted range of a document text comment in a
 * `<mark>` inside the rendered markdown body, so the operator can see at a
 * glance which span their pending comment is anchored to (Google Docs–style
 * yellow highlight on the selection while you compose).
 *
 * Constraints:
 *   - The markdown is rendered as opaque HTML by `MarkdownRenderer`, so we
 *     can't reuse the offsets from the raw revision string. We work over
 *     the rendered DOM's text content instead.
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
 * Walk text nodes inside `root` and return the first contiguous range
 * whose visible text equals `quote`. Skips text inside `<code>`, `<pre>`,
 * `<script>`, `<style>`, and any element that already contains a doc
 * comment mark. Returns `null` if the quote is empty or not found.
 *
 * @param {HTMLElement} root
 * @param {string} quote
 */
function findFirstTextRange(root, quote) {
  if (!root || !quote || typeof window === "undefined") return null;
  const doc =
    root.ownerDocument || (typeof document !== "undefined" ? document : null);
  if (!doc) return null;

  // Build a flat list of (textNode, offsetWithinAggregate) pairs and the
  // aggregate string. Skipping nodes inside code/pre/etc. keeps the
  // mapping stable for the most common comment-on-prose case.
  /** @type {Array<{ node: Text, start: number, end: number }>} */
  const segments = [];
  let aggregate = "";
  const SKIP_TAGS = new Set(["CODE", "PRE", "SCRIPT", "STYLE"]);

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
  if (segments.length === 0) return null;

  const idx = aggregate.indexOf(quote);
  if (idx < 0) return null;
  const endIdx = idx + quote.length;

  let startSeg = null;
  let endSeg = null;
  for (const seg of segments) {
    if (startSeg === null && seg.end > idx) {
      startSeg = seg;
    }
    if (seg.start < endIdx) {
      endSeg = seg;
    }
    if (startSeg && endSeg && seg.start >= endIdx) {
      break;
    }
  }
  if (!startSeg || !endSeg) return null;

  const range = doc.createRange();
  try {
    range.setStart(startSeg.node, idx - startSeg.start);
    range.setEnd(endSeg.node, endIdx - endSeg.start);
  } catch {
    return null;
  }
  return range;
}

/**
 * Wrap the first occurrence of `quote` inside `root` with a `<mark>`
 * element styled as a doc-comment highlight. Replaces any existing
 * doc-comment marks first. Returns `true` if a mark was created.
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
  const range = findFirstTextRange(root, text);
  if (!range) return false;
  const doc = root.ownerDocument;
  if (!doc) return false;
  const mark = doc.createElement("mark");
  mark.className = `${MARK_CLASS} ${
    opts.tone === "posted" ? "is-posted" : "is-pending"
  }`;
  mark.setAttribute(MARK_ATTR, "1");
  if (opts.eventId) {
    mark.setAttribute("data-event-id", String(opts.eventId));
  } else {
    mark.removeAttribute("data-event-id");
  }
  // Setting style here keeps the helper self-contained and avoids
  // requiring the caller to import a stylesheet. Tones:
  //   - pending: solid soft accent fill (you're composing right now)
  //   - posted:  subtle dashed underline (an existing comment lives here)
  if (opts.tone === "posted") {
    mark.style.backgroundColor = "transparent";
    mark.style.borderBottom = "1px dashed var(--accent)";
    mark.style.color = "inherit";
  } else {
    mark.style.backgroundColor =
      "color-mix(in oklab, var(--accent) 22%, transparent)";
    mark.style.color = "inherit";
    mark.style.borderRadius = "2px";
    mark.style.padding = "0 1px";
  }
  try {
    range.surroundContents(mark);
    return true;
  } catch {
    // Range crosses non-text boundaries (e.g. an inline link in the
    // middle of the quote). Fall back to extracting + reinserting.
    try {
      const frag = range.extractContents();
      mark.appendChild(frag);
      range.insertNode(mark);
      return true;
    } catch {
      return false;
    }
  }
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
