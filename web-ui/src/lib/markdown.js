import { Marked } from "marked";

const marked = new Marked({
  gfm: true,
  breaks: false,
});

/**
 * Render markdown string to sanitized HTML.
 * Uses marked with GFM (tables, strikethrough, task lists, autolinks).
 * Output is post-processed to strip dangerous tags/attributes.
 */
export function renderMarkdown(source, { inline = false } = {}) {
  if (!source || typeof source !== "string") return "";
  const raw = inline ? marked.parseInline(source) : marked.parse(source);
  return sanitizeHtml(raw);
}

const ALLOWED_TAGS = new Set([
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
]);

const ALLOWED_ATTRS = new Set([
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
]);

const VOID_TAGS = new Set(["br", "hr", "img", "input"]);

function sanitizeHtml(html) {
  return html.replace(
    /<\/?([a-zA-Z][a-zA-Z0-9]*)\b([^>]*)?\/?>/g,
    (match, tag, attrs) => {
      const lower = tag.toLowerCase();
      if (!ALLOWED_TAGS.has(lower)) return "";

      if (match.startsWith("</")) return `</${lower}>`;

      let safeAttrs = "";
      if (attrs) {
        const attrRegex =
          /([a-zA-Z][a-zA-Z0-9-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|(\S+))/g;
        let attrMatch;
        while ((attrMatch = attrRegex.exec(attrs)) !== null) {
          const name = attrMatch[1].toLowerCase();
          const value = attrMatch[2] ?? attrMatch[3] ?? attrMatch[4] ?? "";
          if (!ALLOWED_ATTRS.has(name)) continue;
          if (name === "href" || name === "src") {
            if (/^\s*javascript:/i.test(value)) continue;
          }
          safeAttrs += ` ${name}="${value.replace(/"/g, "&quot;")}"`;
        }
        const boolAttrs = /\b(checked|disabled)\b/g;
        let boolMatch;
        while ((boolMatch = boolAttrs.exec(attrs)) !== null) {
          if (!safeAttrs.includes(boolMatch[1])) {
            safeAttrs += ` ${boolMatch[1]}`;
          }
        }
      }

      if (lower === "a") {
        if (!safeAttrs.includes("rel=")) {
          safeAttrs += ' rel="noopener noreferrer"';
        }
        if (!safeAttrs.includes("target=")) {
          safeAttrs += ' target="_blank"';
        }
      }

      const selfClose = VOID_TAGS.has(lower) ? " /" : "";
      return `<${lower}${safeAttrs}${selfClose}>`;
    },
  );
}
