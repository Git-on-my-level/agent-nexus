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
