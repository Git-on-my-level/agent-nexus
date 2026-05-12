import DOMPurify from "isomorphic-dompurify";
import { Marked } from "marked";

const marked = new Marked({
  gfm: true,
  breaks: false,
});

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
  const raw = inline ? marked.parseInline(source) : marked.parse(source);
  return sanitizeHtml(raw);
}
