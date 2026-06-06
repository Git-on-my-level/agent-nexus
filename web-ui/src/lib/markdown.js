import DOMPurify from "isomorphic-dompurify";
import { Marked } from "marked";

const marked = new Marked({
  gfm: true,
  breaks: false,
});

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

export function renderMarkdown(source, { inline = false } = {}) {
  if (!source || typeof source !== "string") return "";
  if (inline) return sanitizeHtml(marked.parseInline(source));
  activeHeadingSlugger = createHeadingSlugger();
  let raw;
  try {
    raw = marked.parse(source);
  } finally {
    activeHeadingSlugger = null;
  }
  return sanitizeHtml(raw);
}
