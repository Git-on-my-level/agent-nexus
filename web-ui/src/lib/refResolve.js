/**
 * Ref chips: finding refs, resolving them in one batch, and turning a resolved
 * ref into what a chip and its preview show.
 *
 * Two rules drive the shape of this module:
 *
 * - **No N+1.** A page collects every ref it is about to render, resolves them
 *   in one call, and renders from the result. Chips never fetch for
 *   themselves, which is how the existing event-ref chip behaves and why a
 *   table of twenty rows used to make twenty requests.
 * - **An unresolvable ref is still shown.** A ref that resolves to nothing
 *   renders as a "not found" chip, never as silence. A reader has to be able
 *   to see that a plan points at something missing.
 *
 * The resolved shape mirrors the batch ref resolve contract:
 * `{ref, kind, title, status, phase, owner, progress, url, resolvable}`.
 * Until that endpoint ships, `src/lib/fixtures/refResolveExample.js` provides
 * the same shape so the components are built and tested against it.
 */

import { parseRef } from "./typedRefs.js";
import { workspacePath } from "./workspacePaths.js";

/**
 * Ref prefixes that render as a chip.
 *
 * Both `doc:` and `document:` are accepted, and that is now the contract
 * rather than a hedge: the plan schema names "card:, doc:, document: or topic:"
 * for a step ref, and batch resolve resolves "card, doc/document, topic, and
 * board refs". `board:` has no step-ref use but does resolve, so it chips too.
 */
/** The batch resolve contract takes at most this many refs per request. */
export const MAX_BATCH_REFS = 200;

export const CHIP_REF_PREFIXES = Object.freeze([
  "card",
  "doc",
  "document",
  "topic",
  "board",
]);

/** Operator nouns. `topic:` is a Project and `card:` is a Task to a reader. */
const KIND_LABELS = Object.freeze({
  card: "Task",
  doc: "Doc",
  document: "Doc",
  topic: "Project",
  board: "Board",
  pull_request: "PR",
  issue: "Issue",
});

/** Status to badge tone, using the tones `SignalBadge` already defines. */
const STATUS_TONES = Object.freeze({
  done: "ok",
  cancelled: "neutral",
  blocked: "danger",
  in_progress: "neutral",
  active: "neutral",
  review: "neutral",
  ready: "neutral",
  backlog: "neutral",
  not_started: "neutral",
  unknown: "neutral",
  merged: "ok",
  closed: "neutral",
  open: "neutral",
});

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

/**
 * Every ref a page is about to render, deduped, ready for one batch resolve.
 *
 * @param {Array<string|null|undefined>} texts
 * @param {{ extraRefs?: string[] }} [options] refs already known as refs (plan steps, row source_ids)
 */
export function collectPageRefs(texts = [], { extraRefs = [] } = {}) {
  const refs = new Set();
  for (const ref of extraRefs) {
    const value = asText(ref);
    if (isChipRef(value) || classifyWorkUrl(value)) refs.add(value);
  }
  for (const text of texts) {
    for (const token of tokenizeRefText(text)) {
      if (token.type === "ref") refs.add(token.value);
      if (token.type === "url" && token.kind) refs.add(token.value);
    }
  }
  return [...refs];
}

/**
 * Index a batch resolve response by ref.
 *
 * Rows that came back `resolvable: false` are kept: that is how a chip knows
 * to render "not found" rather than vanishing. A ref that was asked for and is
 * absent from the response entirely is also recorded as unresolvable, so a
 * partial response cannot silently drop a chip.
 *
 * The wire shape is `{items: [...]}` — the batch resolve response — and the
 * contract preserves input order and duplicates, so this indexes rather than
 * assuming uniqueness.
 *
 * @param {{ items?: object[] }|object[]} response
 * @param {string[]} [requested]
 */
export function indexResolvedRefs(response, requested = []) {
  const rows = Array.isArray(response)
    ? response
    : Array.isArray(response?.items)
      ? response.items
      : [];
  const byRef = new Map();
  for (const row of rows) {
    const ref = asText(row?.ref);
    if (!ref) continue;
    byRef.set(ref, {
      ref,
      kind: asText(row?.kind),
      title: asText(row?.title),
      // `status` carries the workflow phase for a chip to show; `phase` is the
      // same value on the derivation path. Either may be absent for a context
      // ref such as a doc or a topic, which has lifecycle state only.
      status: asText(row?.status || row?.phase),
      owner: asText(row?.owner),
      url: asText(row?.url),
      progress: normalizeProgress(row?.progress),
      resolvable: row?.resolvable !== false,
      // Not in the batch resolve contract today. Read defensively so a preview
      // fills in if a later revision adds them, and renders without otherwise.
      board: asText(row?.board ?? row?.board_ref),
      priority: asText(row?.priority),
      lastMovedAt: asText(row?.last_moved_at ?? row?.updated_at),
      nextStep: asText(row?.next_step ?? row?.next_action),
    });
  }
  for (const ref of requested) {
    const value = asText(ref);
    if (value && !byRef.has(value)) {
      byRef.set(value, { ref: value, resolvable: false });
    }
  }
  return byRef;
}

/**
 * "moved 3h ago" — how long since a ref last moved.
 *
 * This repeats the vocabulary of `formatWait` in `inboxMailbox.js` rather than
 * importing it: that module pulls the whole inbox graph in behind it, and a
 * chip rendered on any page should not drag the inbox along. The two want
 * folding into one shared duration helper, which is a change to
 * `inboxMailbox.js` and so is better made once the Overview work has landed
 * than as a conflict now.
 */
export function formatMovedAgo(value, now = Date.now()) {
  const at = value ? new Date(value).getTime() : NaN;
  if (!Number.isFinite(at)) return "";
  const elapsed = Number(now) - at;
  if (elapsed < 0) return "";
  const minutes = Math.floor(elapsed / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function normalizeProgress(value) {
  const done = Number(value?.done);
  const total = Number(value?.total);
  if (!Number.isFinite(done) || !Number.isFinite(total) || total <= 0) {
    return null;
  }
  return { done: Math.max(0, Math.min(done, total)), total };
}

/**
 * Everything a chip and its preview render for one ref.
 *
 * `topic:` chips open the Tasks list filtered to that project, which is a real
 * surface; `board:` has no filter to open, so it gets a title and a copyable
 * ref but no link rather than a 404.
 *
 * @param {string} ref
 * @param {Map<string, object>} resolved
 * @param {{ organizationSlug?: string, workspaceSlug?: string }} [context]
 */
export function refChipModel(ref, resolved, context = {}) {
  const raw = asText(ref);
  const hit = resolved?.get?.(raw) ?? null;
  const external = classifyWorkUrl(raw);
  const { prefix, value } = parseRef(raw);

  const kind = asText(hit?.kind) || external?.kind || prefix;
  const resolvable = hit ? hit.resolvable !== false : Boolean(external);
  const title =
    asText(hit?.title) || external?.label || (resolvable ? raw : "");

  const status = asText(hit?.status);
  return {
    raw,
    kind,
    kindLabel: KIND_LABELS[kind] ?? "",
    title: title || raw,
    status,
    statusLabel: status ? status.replaceAll("_", " ") : "",
    statusTone: STATUS_TONES[status] ?? "neutral",
    owner: asText(hit?.owner),
    priority: asText(hit?.priority),
    board: asText(hit?.board),
    nextStep: asText(hit?.nextStep),
    lastMovedAt: asText(hit?.lastMovedAt),
    progress: hit?.progress ?? null,
    resolvable,
    isExternal: Boolean(external),
    href: refHref({ raw, prefix, value, hit, external, context, resolvable }),
  };
}

/**
 * Destinations a chip is allowed to link to: an absolute http(s) URL, or a
 * workspace-relative path. Anything else — `javascript:`, `data:`, a
 * protocol-relative `//host` that leaves the origin — is refused and the chip
 * renders unlinked rather than becoming an executable anchor.
 *
 * Resolved refs come from core today, but a chip is a generic renderer and the
 * cost of being wrong here is an executable link, so the check lives at the
 * point of rendering rather than relying on the producer.
 */
export function safeRefDestination(value) {
  const url = asText(value);
  if (!url) return "";
  if (/^https?:\/\//i.test(url)) return url;
  // A single leading slash only: `//evil.test` is a protocol-relative URL.
  if (/^\/(?!\/)/.test(url)) return url;
  return "";
}

function refHref({ raw, prefix, value, hit, external, context, resolvable }) {
  // Nothing to open: a "not found" chip must not offer a link into a 404.
  if (!resolvable) return "";
  if (external) return safeRefDestination(raw);

  const org = asText(context.organizationSlug);
  const workspace = asText(context.workspaceSlug);

  // Core returns a path relative to the workspace (`/tasks/<handle>`), not a
  // routable one. The UI's routes are `/o/<org>/w/<workspace>/…` under the app
  // base path, so a server path has to be rebased or it navigates out of the
  // workspace the reader is looking at.
  const explicit = safeRefDestination(hit?.url);
  if (explicit) {
    if (/^https?:\/\//i.test(explicit)) return explicit;
    if (!org || !workspace) return "";
    return workspacePath(org, workspace, explicit);
  }

  if (!org || !workspace || !value) return "";

  if (prefix === "card") {
    return workspacePath(
      org,
      workspace,
      `/tasks/${encodeURIComponent(`card:${value}`)}`,
    );
  }
  if (prefix === "doc" || prefix === "document") {
    return workspacePath(org, workspace, `/docs/${encodeURIComponent(value)}`);
  }
  if (prefix === "topic") {
    // Projects have no page of their own; the Tasks list filtered to the
    // project is the surface that answers "what is in it".
    return workspacePath(
      org,
      workspace,
      `/tasks?project_ref=${encodeURIComponent(`topic:${value}`)}`,
    );
  }
  return "";
}
