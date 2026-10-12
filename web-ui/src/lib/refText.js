/**
 * Finding refs in text.
 *
 * The pure half of ref chips: which prefixes chip, which URLs stand for work,
 * and how a run of prose splits into plain text and ref tokens. No routing, no
 * fetching, no workspace paths — nothing that needs SvelteKit.
 *
 * That constraint is load-bearing rather than tidiness. `markdown.js` scans
 * prose for refs, and `scripts/check-visual-report-conformance.mjs` imports
 * `markdown.js` straight into plain Node to check the browser's summary parser
 * against the Go one. A `$app/paths` anywhere in that import chain is not a
 * resolvable module there, so the check dies before it runs. `refResolve.js`
 * re-exports everything here, so callers that want the whole chip model —
 * titles, status, hrefs — keep importing from there.
 */

import { parseRef } from "./typedRefs.js";

/**
 * Ref prefixes that render as a chip.
 *
 * Both `doc:` and `document:` are accepted, and that is now the contract
 * rather than a hedge: the plan schema names "card:, doc:, document: or topic:"
 * for a step ref, and batch resolve resolves "card, doc/document, topic, and
 * board refs". `board:` has no step-ref use but does resolve, so it chips too.
 */
export const CHIP_REF_PREFIXES = Object.freeze([
  "card",
  "doc",
  "document",
  "topic",
  "board",
]);

const asText = (value) => String(value ?? "").trim();

/**
 * A typed ref inside prose: `card:release-b`, `doc:plan`, and so on.
 *
 * The value charset matches the board and project ref patterns the live query
 * contract validates, so this finds the same refs the server would accept. A
 * trailing `.`, `,` or `)` belongs to the sentence, not the ref, so the
 * charset deliberately excludes them at the end.
 */
const TYPED_REF_RE = new RegExp(
  `\\b(${CHIP_REF_PREFIXES.join("|")}):([A-Za-z0-9][A-Za-z0-9._-]{0,127})`,
  "g",
);

/**
 * External URLs a reader treats as work: a GitHub pull request or issue, and a
 * Multica issue.
 *
 * Multica deployments are self-hosted on arbitrary hosts (a tailnet name, for
 * instance), so its matcher keys on the path shape rather than a host. That
 * means a same-shaped path on an unrelated host also matches; the chip only
 * ever labels and links such a URL, so the cost of a false positive is a
 * mislabelled link rather than a bad request.
 */
const URL_MATCHERS = Object.freeze([
  {
    kind: "pull_request",
    source: "github",
    pattern:
      /^https:\/\/github\.com\/([^/\s]+)\/([^/\s]+)\/pull\/(\d+)(?:[^\s]*)?$/i,
    label: (match) => `${match[1]}/${match[2]}#${match[3]}`,
  },
  {
    kind: "issue",
    source: "github",
    pattern:
      /^https:\/\/github\.com\/([^/\s]+)\/([^/\s]+)\/issues\/(\d+)(?:[^\s]*)?$/i,
    label: (match) => `${match[1]}/${match[2]}#${match[3]}`,
  },
  {
    kind: "issue",
    source: "multica",
    pattern:
      /^https?:\/\/[^/\s]+\/(?:issue|issues)\/([A-Za-z][A-Za-z0-9]*-\d+)(?:[^\s]*)?$/,
    label: (match) => match[1].toUpperCase(),
  },
]);

/**
 * Recognise a URL that stands for a piece of work.
 * @returns {{kind: string, source: string, label: string, url: string}|null}
 */
export function classifyWorkUrl(value) {
  const url = asText(value);
  if (!url) return null;
  for (const matcher of URL_MATCHERS) {
    const match = matcher.pattern.exec(url);
    if (match) {
      return {
        kind: matcher.kind,
        source: matcher.source,
        label: matcher.label(match),
        url,
      };
    }
  }
  return null;
}

/** How long a shortened link label may get before it is clipped. */
const LINK_LABEL_MAX = 44;

/**
 * A short, readable name for a link that no matcher above recognises.
 *
 * A plan step may point at any HTTP(S) URL the plan contract accepts, and most
 * of them are not pull requests: a repository, a runbook, a design doc. Those
 * used to render as the whole URL, which on a phone is most of a line of text
 * for a destination the reader cannot read anyway. So the label is the host and
 * the part of the path that names the thing — the repo, the file — and the full
 * URL stays on the chip's `title`, where a reader who wants it can hover.
 *
 * `…` marks a dropped middle rather than a dropped end: the last segment is
 * usually the name, so `github.com/…/plan.md` beats `github.com/Git-on-my-l…`.
 */
export function shortLinkLabel(value) {
  const raw = asText(value);
  if (!raw) return "";
  let url;
  try {
    url = new URL(raw);
  } catch {
    return raw;
  }
  const host = url.hostname.replace(/^www\./i, "");
  const segments = url.pathname.split("/").filter(Boolean);
  const path =
    segments.length <= 2
      ? segments.join("/")
      : `…/${segments[segments.length - 1]}`;
  const label = path ? `${host}/${path}` : host;
  return label.length > LINK_LABEL_MAX
    ? `${label.slice(0, LINK_LABEL_MAX - 1)}…`
    : label;
}

/**
 * Any safe HTTP(S) link, as the same `{kind, source, label, url}` shape
 * `classifyWorkUrl` returns for the URLs it recognises.
 *
 * Deliberately separate from `classifyWorkUrl`: that answers "is this URL a
 * piece of work", which is what decides whether a bare URL in prose becomes a
 * chip at all, and every URL is not. This answers the narrower question a chip
 * already rendering a known ref has — "what do I call this link" — so it is for
 * callers handed a ref, not for scanning text.
 *
 * @returns {{kind: string, source: string, label: string, url: string}|null}
 */
export function classifyLink(value) {
  const url = asText(value);
  if (!/^https?:\/\//i.test(url)) return null;
  const label = shortLinkLabel(url);
  if (!label) return null;
  return { kind: "link", source: "", label, url };
}

/** A ref-shaped token, so callers can test one string without scanning. */
export function isChipRef(value) {
  const { prefix, value: id } = parseRef(asText(value));
  return CHIP_REF_PREFIXES.includes(prefix) && Boolean(id);
}

/**
 * Split text into plain runs and ref/URL tokens, in order, so a renderer can
 * emit chips inside a table cell, a callout or a node label without building
 * markup from the text.
 *
 * @param {string} text
 * @returns {Array<{type: "text"|"ref"|"url", value: string, kind?: string, label?: string}>}
 */
export function tokenizeRefText(text) {
  const source = String(text ?? "");
  if (!source) return [];

  const found = [];
  for (const match of source.matchAll(TYPED_REF_RE)) {
    // `.`, `-` and `_` are legal inside a ref value but a run of them at the
    // end is the sentence, not the ref: "fixed card:release-b." must not chip
    // a ref called `release-b.` and then fail to resolve it.
    const value = match[0].replace(/[._-]+$/, "");
    if (!value.endsWith(":")) {
      found.push({
        start: match.index,
        end: match.index + value.length,
        token: { type: "ref", value },
      });
    }
  }
  // Bare URLs are matched on whitespace-delimited runs so a trailing comma or
  // bracket stays in the sentence.
  for (const match of source.matchAll(/https?:\/\/[^\s<>"]+/g)) {
    const raw = match[0].replace(/[.,;:)\]}>]+$/, "");
    const classified = classifyWorkUrl(raw);
    found.push({
      start: match.index,
      end: match.index + raw.length,
      token: {
        type: "url",
        value: raw,
        ...(classified
          ? { kind: classified.kind, label: classified.label }
          : {}),
      },
    });
  }

  found.sort((a, b) => a.start - b.start);

  const tokens = [];
  let cursor = 0;
  for (const entry of found) {
    if (entry.start < cursor) continue;
    if (entry.start > cursor) {
      tokens.push({ type: "text", value: source.slice(cursor, entry.start) });
    }
    tokens.push(entry.token);
    cursor = entry.end;
  }
  if (cursor < source.length) {
    tokens.push({ type: "text", value: source.slice(cursor) });
  }
  return tokens;
}
