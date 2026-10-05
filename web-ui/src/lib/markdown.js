import DOMPurify from "isomorphic-dompurify";
import { Marked } from "marked";

import { classifyWorkUrl, tokenizeRefText } from "./refResolve.js";

const marked = new Marked({
  gfm: true,
  breaks: false,
});

/**
 * Count GFM task-list items using the same CommonMark parser as rendering.
 * Code blocks are opaque lexer tokens, so checkbox examples inside them do not
 * contribute to progress.
 *
 * @param {string} source
 * @returns {{ done: number, total: number }}
 */
export function countMarkdownTaskProgress(source) {
  const progress = { done: 0, total: 0 };
  if (typeof source !== "string" || source === "") return progress;

  function visit(tokens) {
    for (const token of tokens ?? []) {
      if (token?.type === "code") continue;
      if (token?.type === "list") {
        for (const item of token.items ?? []) {
          // Marked's GFM task tokenizer does not recognize a checkbox-only
          // first line when the list item continues on the next line. The
          // CommonMark list AST still gives us the item boundary, so recognize
          // a task marker at the start of that item's text as well.
          const leadingTask = /^\[([ xX])\](?=$|[ \t\n])/.exec(item.text ?? "");
          if (item.task || leadingTask) {
            progress.total++;
            if (
              item.task ? item.checked : leadingTask?.[1]?.toLowerCase() === "x"
            )
              progress.done++;
          }
          visit(item.tokens);
        }
      }
      visit(token?.tokens);
    }
  }

  try {
    visit(marked.lexer(source));
  } catch {
    return { done: 0, total: 0 };
  }
  return progress;
}

/**
 * Build a slug generator that mirrors GitHub-style heading anchors and
 * de-duplicates repeats within a single document (e.g. two "Notes" headings
 * become `notes` and `notes-1`). A fresh slugger must be used per parse so the
 * suffix counters stay aligned between the rendered HTML and any derived
 * table-of-contents.
 */
function createHeadingSlugger() {
  const seen = new Map();
  return (raw) => {
    const base =
      String(raw ?? "")
        .toLowerCase()
        .trim()
        .replace(/[^\w\s-]/g, "")
        .replace(/\s+/g, "-")
        .replace(/-+/g, "-")
        .replace(/^-+|-+$/g, "") || "section";
    const count = seen.get(base) ?? 0;
    seen.set(base, count + 1);
    return count === 0 ? base : `${base}-${count}`;
  };
}

/** Strip common inline markdown so outline labels read as plain prose. */
function headingDisplayText(raw) {
  return String(raw ?? "")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/\*([^*]+)\*/g, "$1")
    .replace(/__([^_]+)__/g, "$1")
    .replace(/_([^_]+)_/g, "$1")
    .replace(/~~([^~]+)~~/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .trim();
}

// Active slugger for the in-progress `marked.parse` call. `marked.parse` is
// synchronous, so we set this immediately before parsing and clear it after.
let activeHeadingSlugger = null;

marked.use({
  renderer: {
    /** @param {{ depth?: number, tokens?: unknown[], text?: string }} token */
    heading(token) {
      const depth =
        typeof token?.depth === "number" && token.depth >= 1 && token.depth <= 6
          ? token.depth
          : 1;
      const inlineHtml = token?.tokens
        ? this.parser.parseInline(token.tokens)
        : String(token?.text ?? "");
      const slugger = activeHeadingSlugger ?? createHeadingSlugger();
      const id = slugger(token?.text ?? "");
      return `<h${depth} id="${id}">${inlineHtml}</h${depth}>\n`;
    },
  },
});

/**
 * Ref chips inside prose.
 *
 * `card:release-b` written in a card body, a doc or an ask should read as the
 * same chip it reads as everywhere else, and so should a bare GitHub pull
 * request or issue URL. The renderer cannot mount a Svelte component, so it
 * emits a placeholder — `<span class="md-ref" data-md-ref="…">` carrying the
 * raw ref — and `MarkdownRenderer` replaces each one with the real chip. The
 * placeholder's own text is the raw ref, so server-rendered and no-JS output
 * still says what the chip points at.
 *
 * Chips are suppressed inside a link: an `<a>` inside an `<a>` is not markup a
 * browser can nest, and the link already names its destination.
 */
const REF_PLACEHOLDER_CLASS = "md-ref";

/** Attribute-safe text. Ref values are narrow, but a URL is not. */
function attrValue(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function refPlaceholder(value) {
  const raw = String(value ?? "");
  const label = classifyWorkUrl(raw)?.label || raw;
  return `<span class="${REF_PLACEHOLDER_CLASS}" data-md-ref="${attrValue(raw)}">${attrValue(label)}</span>`;
}

/**
 * Per-parse renderer state. `marked.parse` is synchronous, so a module-level
 * flag is safe here the same way `activeHeadingSlugger` is.
 */
let refChipsEnabled = false;
/** Depth of the link currently being rendered; chips are off inside one. */
let linkDepth = 0;

/** Chip markup for the ref and work-URL tokens in one run of plain text. */
function withRefPlaceholders(text) {
  const source = String(text ?? "");
  if (!source) return "";
  const tokens = tokenizeRefText(source);
  // A run with nothing to chip is returned untouched rather than reassembled.
  if (!tokens.some((token) => token.type === "ref" || token.kind)) {
    return source;
  }
  return tokens
    .map((token) =>
      token.type === "ref" || (token.type === "url" && token.kind)
        ? refPlaceholder(token.value)
        : token.value,
    )
    .join("");
}

marked.use({
  renderer: {
    /** @param {{ tokens?: unknown[], text?: string }} token */
    text(token) {
      if (token?.tokens) return this.parser.parseInline(token.tokens);
      const raw = String(token?.text ?? "");
      if (!refChipsEnabled || linkDepth > 0) return raw;
      return withRefPlaceholders(raw);
    },
    /** @param {{ href?: string, title?: string, text?: string, tokens?: unknown[] }} token */
    link(token) {
      const href = String(token?.href ?? "");
      const text = String(token?.text ?? "");
      // GFM turns a bare work URL into a link whose text is the URL. That is
      // the autolink case, and it becomes a chip; a link someone wrote with
      // their own label keeps the label they chose.
      if (refChipsEnabled && linkDepth === 0 && text === href) {
        if (classifyWorkUrl(href)) return refPlaceholder(href);
      }
      linkDepth += 1;
      try {
        const inner = token?.tokens
          ? this.parser.parseInline(token.tokens)
          : text;
        const title = token?.title ? ` title="${attrValue(token.title)}"` : "";
        return `<a href="${attrValue(href)}"${title}>${inner}</a>`;
      } finally {
        linkDepth -= 1;
      }
    },
  },
});

/**
 * Extract an ordered outline (H1-H3) from markdown source for a document
 * table-of-contents. Ids match the anchors emitted by `renderMarkdown` so
 * clicking an entry can scroll to the heading.
 *
 * @param {string} source
 * @returns {Array<{ level: number, text: string, id: string }>}
 */
export function extractDocumentOutline(source) {
  if (!source || typeof source !== "string") return [];
  let tokens;
  try {
    tokens = marked.lexer(source);
  } catch {
    return [];
  }
  const slugger = createHeadingSlugger();
  const outline = [];
  for (const token of tokens) {
    if (token?.type !== "heading") continue;
    const depth = Number(token.depth);
    // Slug every heading (1-6) so dedupe counters stay aligned with the
    // renderer, but only surface H1-H3 in the outline.
    const id = slugger(token.text ?? "");
    if (depth < 1 || depth > 3) continue;
    const text = headingDisplayText(token.text ?? "");
    if (!text) continue;
    outline.push({ level: depth, text, id });
  }
  return outline;
}

const ALLOWED_TAGS = [
  // `<details>/<summary>` is the one block of raw HTML an author is expected
  // to write: it is how a long aside stays out of the way. It renders;
  // everything else HTML-shaped is still stripped.
  "details",
  "summary",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "p",
  "br",
  "hr",
  "ul",
  "ol",
  "li",
  "blockquote",
  "pre",
  "code",
  "em",
  "strong",
  "del",
  "a",
  "img",
  "table",
  "thead",
  "tbody",
  "tr",
  "th",
  "td",
  "input",
  "span",
  "div",
  "sup",
  "sub",
];

const ALLOWED_ATTRS = [
  // The ref-chip placeholder `MarkdownRenderer` hydrates. Listed explicitly
  // rather than by turning `ALLOW_DATA_ATTR` back on. It is deliberately not
  // `data-anx-ref`: that attribute means "this element is a chip", and a
  // placeholder still carrying it would make every chip query find two.
  "data-md-ref",
  "open",
  "href",
  "title",
  "alt",
  "src",
  "class",
  "id",
  "type",
  "checked",
  "disabled",
  "align",
];

const LOCAL_IMAGE_SRC_RE = /^(?:\/(?!\/)|\.{0,2}\/|[^:/?#]+(?:[/?#]|$))/;
const NON_NETWORK_IMAGE_SRC_RE = /^(?:data:image\/|blob:)/i;

const purifyConfig = {
  ALLOWED_TAGS,
  ALLOWED_ATTR: ALLOWED_ATTRS,
  ALLOW_DATA_ATTR: false,
  ADD_ATTR: ["target"],
  FORBID_TAGS: ["script", "iframe", "object", "embed", "form"],
  FORBID_ATTR: [
    "onerror",
    "onload",
    "onclick",
    "onmouseover",
    "onfocus",
    "onblur",
  ],
  ADD_DATA_URI_TAGS: ["img"],
  // The chip placeholder carries a ref, not a URL: `card:release-b` is not a
  // scheme `ALLOWED_URI_REGEXP` accepts, so without this the attribute is
  // dropped and the chip never mounts. The value never becomes an href —
  // `refChipModel` runs it through `safeRefDestination` first.
  ADD_URI_SAFE_ATTR: ["data-md-ref"],
  ALLOWED_URI_REGEXP:
    /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|sms|cid|xmpp):|[^a-z]|[a-z+.-]+(?:[^a-z+.\-:]|$))/i,
};

function isAllowedMarkdownImageSrc(value) {
  const src = String(value ?? "").trim();
  if (!src) return false;
  return NON_NETWORK_IMAGE_SRC_RE.test(src) || LOCAL_IMAGE_SRC_RE.test(src);
}

DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  if (node?.nodeName !== "IMG") return;

  const src = node.getAttribute("src");
  if (!isAllowedMarkdownImageSrc(src)) {
    node.remove();
  }
});

function sanitizeHtml(html) {
  const sanitized = DOMPurify.sanitize(html, purifyConfig);

  return sanitized.replace(/<a\b([^>]*)>/gi, (match, attrs) => {
    let updated = attrs;

    if (!/\brel\s*=/.test(updated)) {
      updated += ' rel="noopener noreferrer"';
    }

    if (!/\btarget\s*=/.test(updated)) {
      updated += ' target="_blank"';
    }

    return `<a${updated}>`;
  });
}

/**
 * The one markdown renderer. Every surface that shows authored text — card
 * bodies, docs, reports, ask text, tile descriptions, previews — goes through
 * here, via `MarkdownRenderer`.
 *
 * @param {string} source
 * @param {{ inline?: boolean, refChips?: boolean }} [options]
 *   `inline` renders a single run with no block wrapper. `refChips` emits the
 *   chip placeholders `MarkdownRenderer` hydrates; turn it off for a surface
 *   that has no resolved refs to hydrate them with.
 */
export function renderMarkdown(
  source,
  { inline = false, refChips = true } = {},
) {
  if (!source || typeof source !== "string") return "";
  refChipsEnabled = refChips !== false;
  linkDepth = 0;
  activeHeadingSlugger = inline ? null : createHeadingSlugger();
  let raw;
  try {
    raw = inline ? marked.parseInline(source) : marked.parse(source);
  } finally {
    activeHeadingSlugger = null;
    refChipsEnabled = false;
    linkDepth = 0;
  }
  return sanitizeHtml(raw);
}

/**
 * Inline tokens as the prose a reader would say out loud: no emphasis markers,
 * no link syntax, no raw HTML. A code span keeps its text, because that text is
 * usually the thing being named.
 */
function inlineTokenText(tokens) {
  let out = "";
  for (const token of tokens ?? []) {
    if (!token) continue;
    switch (token.type) {
      case "html":
        break;
      case "br":
        out += " ";
        break;
      case "codespan":
      case "escape":
        out += String(token.text ?? "");
        break;
      default:
        out += token.tokens
          ? inlineTokenText(token.tokens)
          : String(token.text ?? "");
    }
  }
  return out;
}

/** Block-level tokens that carry prose a one-line excerpt can be drawn from. */
const EXCERPT_BLOCKS = new Set([
  "paragraph",
  "text",
  "heading",
  "blockquote",
  "list",
]);

function blockText(token) {
  if (!token) return "";
  if (token.type === "list") {
    for (const item of token.items ?? []) {
      // A task-list marker is structure, not prose.
      const text = item?.tokens
        ? inlineTokenText(item.tokens)
        : String(item?.text ?? "");
      const cleaned = text.replace(/^\[[ xX]\]\s*/, "").trim();
      if (cleaned) return cleaned;
    }
    return "";
  }
  if (token.type === "blockquote") {
    for (const child of token.tokens ?? []) {
      const text = blockText(child);
      if (text) return text;
    }
    return "";
  }
  return token.tokens
    ? inlineTokenText(token.tokens)
    : String(token.text ?? "");
}

const collapseWhitespace = (value) =>
  String(value ?? "")
    .replace(/\s+/g, " ")
    .trim();

/**
 * A one-line plain-text excerpt of markdown source.
 *
 * Tiles, rows and previews show a single line of an authored body. Rendering
 * markdown into that line is wrong twice over: a tile cannot show a table, and
 * the raw source leaks syntax, which is why tiles used to read `**Goal:** …`.
 * This takes the first block that carries prose — skipping fenced code, HTML
 * blocks and horizontal rules — and flattens it.
 *
 * @param {string} source
 * @param {{ limit?: number }} [options]
 */
export function markdownExcerpt(source, { limit = 180 } = {}) {
  if (!source || typeof source !== "string") return "";
  let tokens;
  try {
    tokens = marked.lexer(source);
  } catch {
    return collapseWhitespace(source).slice(0, limit);
  }
  let excerpt = "";
  for (const token of tokens) {
    if (!EXCERPT_BLOCKS.has(token?.type)) continue;
    excerpt = collapseWhitespace(blockText(token));
    if (excerpt) break;
  }
  if (!excerpt) return "";
  if (!Number.isFinite(limit) || limit <= 0 || excerpt.length <= limit) {
    return excerpt;
  }
  const clipped = excerpt.slice(0, limit);
  const lastSpace = clipped.lastIndexOf(" ");
  return `${(lastSpace > limit * 0.6 ? clipped.slice(0, lastSpace) : clipped).trimEnd()}…`;
}

/**
 * Markdown source as plain text, blocks separated by a single space. For an
 * `aria-label`, a `title` or a search index — anywhere the words are wanted
 * without the markup.
 *
 * @param {string} source
 */
export function markdownPlainText(source) {
  if (!source || typeof source !== "string") return "";
  let tokens;
  try {
    tokens = marked.lexer(source);
  } catch {
    return collapseWhitespace(source);
  }
  const parts = [];
  for (const token of tokens) {
    if (token?.type === "code") {
      parts.push(String(token.text ?? ""));
      continue;
    }
    if (!EXCERPT_BLOCKS.has(token?.type)) continue;
    if (token.type === "list") {
      for (const item of token.items ?? []) {
        const text = item?.tokens
          ? inlineTokenText(item.tokens)
          : String(item?.text ?? "");
        parts.push(text.replace(/^\[[ xX]\]\s*/, ""));
      }
      continue;
    }
    parts.push(blockText(token));
  }
  return collapseWhitespace(parts.join(" "));
}
