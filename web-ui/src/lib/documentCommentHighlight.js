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
 *   - Multiple posted comments on the *same* quote are merged into one
 *     span; overlapping comments with *different* quotes (one range inside
 *     another) are decomposed so each sub-range gets the right set of
 *     `data-event-id` / `data-event-ids` and stacked underlines
 *     (see `decomposeToAtomicHighlights` + `styleForPostedStack`).
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
 * @param {{ throughDocMarks?: boolean }} [opts] when `true`, recurse into our
 *   `data-doc-comment-mark` elements so text offsets still match the document
 *   after earlier wraps (used when applying several ranges in one pass).
 */
function collectTextSegments(root, opts = {}) {
  const throughDocMarks = Boolean(opts.throughDocMarks);
  /** @type {Array<{ node: Text, start: number, end: number }>} */
  const segments = [];
  let aggregate = "";

  /** @param {Node} node */
  function visit(node) {
    if (node.nodeType === 1) {
      const el = /** @type {Element} */ (node);
      if (SKIP_TAGS.has(el.tagName)) return;
      if (!throughDocMarks && el.hasAttribute && el.hasAttribute(MARK_ATTR)) {
        return;
      }
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
 * Build the inline style for a posted doc-comment `<mark>`.
 * One dashed underline; when several comments share the same quote, draw
 * parallel dashed lines with a slight vertical offset so all are visible.
 *
 * @param {number} stackDepth
 */
function styleForPostedStack(stackDepth) {
  const bg = "color-mix(in oklab, var(--accent) 8%, transparent)";
  const d = !Number.isFinite(stackDepth) || stackDepth < 1 ? 1 : stackDepth;
  const base = {
    backgroundColor: bg,
    color: "inherit",
    borderRadius: "2px",
    padding: "0 1px",
    cursor: "pointer",
  };
  if (d === 1) {
    return {
      ...base,
      borderBottom: "1px dashed var(--accent)",
    };
  }
  // Always keep a real border under inline marks — `backgroundImage`-only
  // underlines are easy to miss on split text nodes or when layers paint oddly.
  const layers = [];
  for (let j = 0; j < d - 1; j++) {
    const offset = 3 + j * 2;
    layers.push(
      `repeating-linear-gradient(90deg, var(--accent) 0 3px, transparent 3px 6px) 0 calc(100% - ${offset}px) / 100% 1px no-repeat`,
    );
  }
  return {
    ...base,
    borderBottom: "1px dashed var(--accent)",
    backgroundImage: layers.join(", "),
  };
}

/**
 * @param {"pending" | "posted"} tone
 * @param {number} [postedStackDepth=1] used when tone is `posted`
 */
function styleForTone(tone, postedStackDepth = 1) {
  if (tone === "posted") {
    return styleForPostedStack(postedStackDepth);
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
 * @param {{ tone: "pending" | "posted", eventId?: string, eventIds?: string[], stackDepth?: number }} opts
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
  const eids =
    Array.isArray(opts.eventIds) && opts.eventIds.length
      ? opts.eventIds.map((x) => String(x).trim()).filter(Boolean)
      : opts.eventId
        ? [String(opts.eventId).trim()]
        : [];
  if (eids[0]) {
    mark.setAttribute("data-event-id", eids[0]);
  }
  if (eids.length > 1) {
    mark.setAttribute("data-event-ids", eids.join(" "));
  }
  const stackDepth = opts.stackDepth ?? (eids.length > 0 ? eids.length : 1);
  const style = styleForTone(
    opts.tone,
    opts.tone === "posted" ? stackDepth : 1,
  );
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
 * @param {Array<{ quote: string, eventId?: string }>} posted
 * @param {string} pendingTrim
 * @param {string} aggregate
 */
function buildPostedAnchorRanges(posted, pendingTrim, aggregate) {
  /** @type {Map<string, { start: number, end: number, eventIds: string[] }>} */
  const spanTo = new Map();
  for (const row of posted) {
    const q = String(row?.quote ?? "").trim();
    if (!q) continue;
    if (pendingTrim && q === pendingTrim) continue;
    const id = String(row?.eventId ?? "").trim();
    if (!id) continue;
    const idx = aggregate.indexOf(q);
    if (idx < 0) continue;
    const start = idx;
    const end = idx + q.length;
    const key = `${start}:${end}`;
    const cur = spanTo.get(key) ?? { start, end, eventIds: [] };
    if (!cur.eventIds.includes(id)) {
      cur.eventIds.push(id);
    }
    spanTo.set(key, cur);
  }
  return Array.from(spanTo.values());
}

/**
 * @param {string} aggregate
 * @param {string} pendingTrim
 * @returns {{ start: number, end: number } | null}
 */
function pendingOffsetRange(aggregate, pendingTrim) {
  if (!pendingTrim) return null;
  const idx = aggregate.indexOf(pendingTrim);
  if (idx < 0) return null;
  return { start: idx, end: idx + pendingTrim.length };
}

/**
 * Cut [0, aggregateLength) at every posted/pending range boundary, then
 * for each sub-interval decide which event ids (if any) apply and whether
 * pending style wins for that run.
 *
 * @param {Array<{ start: number, end: number, eventIds: string[] }>} postedRanges
 * @param {{ start: number, end: number } | null} pendingR
 * @param {number} aggregateLength
 */
function decomposeToAtomicHighlights(postedRanges, pendingR, aggregateLength) {
  const breaks = new Set([0, aggregateLength]);
  for (const r of postedRanges) {
    if (r.start < r.end) {
      breaks.add(r.start);
      breaks.add(r.end);
    }
  }
  if (pendingR && pendingR.start < pendingR.end) {
    breaks.add(pendingR.start);
    breaks.add(pendingR.end);
  }
  const bp = Array.from(breaks)
    .filter((n) => n >= 0 && n <= aggregateLength)
    .sort((a, b) => a - b);

  /** @type {Array<{ start: number, end: number, eventIds: string[], tone: "posted" | "pending" }>} */
  const out = [];
  for (let i = 0; i < bp.length - 1; i++) {
    const a = bp[i];
    const b = bp[i + 1];
    if (a >= b) continue;

    const pendingCovers =
      pendingR != null && pendingR.start <= a && pendingR.end >= b;

    if (pendingCovers) {
      out.push({ start: a, end: b, eventIds: [], tone: "pending" });
      continue;
    }

    /** @type {string[]} */
    const postedIds = [];
    for (const r of postedRanges) {
      if (r.start <= a && r.end >= b) {
        for (const id of r.eventIds) {
          if (!postedIds.includes(id)) {
            postedIds.push(id);
          }
        }
      }
    }
    if (postedIds.length > 0) {
      out.push({ start: a, end: b, eventIds: postedIds, tone: "posted" });
    }
  }
  return out;
}

/**
 * Like `highlightDocumentCommentRange` with explicit [start, end) offsets, but
 * after earlier highlights exist on `root` (uses `throughDocMarks` so offsets
 * still align with the full document text).
 *
 * @param {HTMLElement} root
 * @param {number} start
 * @param {number} end
 * @param {{ tone: "pending" | "posted", eventId?: string, eventIds?: string[], stackDepth?: number }} wrapOpts
 */
function wrapAggregateOffsetRange(root, start, end, wrapOpts) {
  if (!root) return false;
  const doc = root.ownerDocument;
  if (!doc) return false;
  const { segments, aggregate } = collectTextSegments(
    /** @type {HTMLElement} */ (root),
    { throughDocMarks: true },
  );
  if (start < 0 || end > aggregate.length || start >= end) {
    return false;
  }
  /** @type {Array<{ node: Text, start: number, end: number }>} */
  const overlaps = [];
  for (const seg of segments) {
    if (seg.end <= start) {
      continue;
    }
    if (seg.start >= end) {
      break;
    }
    const startInNode = Math.max(0, start - seg.start);
    const endInNode = Math.min(seg.end - seg.start, end - seg.start);
    if (endInNode > startInNode) {
      overlaps.push({ node: seg.node, start: startInNode, end: endInNode });
    }
  }
  let any = false;
  for (const piece of overlaps) {
    const m = wrapTextNodeSlice(
      doc,
      piece.node,
      piece.start,
      piece.end,
      wrapOpts,
    );
    if (m) {
      any = true;
    }
  }
  return any;
}

/**
 * Wrap the first occurrence of `quote` inside `root` with one `<mark>` per
 * text node fragment that the quote covers. Replaces existing doc-comment
 * marks first unless `opts.clear === false`. Returns `true` if any mark was
 * created.
 *
 * @param {HTMLElement | null | undefined} root
 * @param {string} quote
 * @param {{ tone?: "pending" | "posted", eventId?: string, eventIds?: string[], clear?: boolean }} [opts]
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
  const eids = Array.isArray(opts.eventIds)
    ? opts.eventIds.map((x) => String(x).trim()).filter(Boolean)
    : opts.eventId
      ? [String(opts.eventId).trim()]
      : [];
  const wrapOpts = {
    tone,
    eventId: eids[0] ?? "",
    eventIds: eids,
    stackDepth: eids.length > 0 ? eids.length : 1,
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
  const el = /** @type {HTMLElement} */ (root);
  const { aggregate } = collectTextSegments(el);
  const postedRanges = buildPostedAnchorRanges(posted, pendingTrim, aggregate);
  const pendingR = pendingOffsetRange(aggregate, pendingTrim);
  const atoms = decomposeToAtomicHighlights(
    postedRanges,
    pendingR,
    aggregate.length,
  );
  for (const atom of atoms) {
    if (atom.tone === "pending") {
      wrapAggregateOffsetRange(el, atom.start, atom.end, {
        tone: "pending",
        eventIds: [],
        stackDepth: 1,
      });
    } else {
      const eids = atom.eventIds;
      wrapAggregateOffsetRange(el, atom.start, atom.end, {
        tone: "posted",
        eventId: eids[0] ?? "",
        eventIds: eids,
        stackDepth: eids.length > 0 ? eids.length : 1,
      });
    }
  }
}

/**
 * @param {Element} el
 * @returns {string[]}
 */
export function eventIdsFromDocCommentMark(el) {
  if (!el || typeof el.getAttribute !== "function") {
    return [];
  }
  const many = el.getAttribute("data-event-ids");
  if (many && many.trim()) {
    return many.trim().split(/\s+/).filter(Boolean);
  }
  const one = el.getAttribute("data-event-id");
  return one && one.trim() ? [one.trim()] : [];
}
