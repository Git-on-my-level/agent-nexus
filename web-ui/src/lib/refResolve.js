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

import {
  CHIP_REF_PREFIXES,
  classifyWorkUrl,
  isChipRef,
  tokenizeRefText,
} from "./refText.js";
import { parseRef } from "./typedRefs.js";
import { workspacePath } from "./workspacePaths.js";

/*
 * Finding refs in text lives in `refText.js`, which is deliberately free of
 * `$app` so `markdown.js` can scan prose without dragging SvelteKit into the
 * plain-Node report conformance check. Re-exported here because this is the
 * module callers already import ref vocabulary from.
 */
export { CHIP_REF_PREFIXES, classifyWorkUrl, isChipRef, tokenizeRefText };

/** The batch resolve contract takes at most this many refs per request. */
export const MAX_BATCH_REFS = 200;

const asText = (value) => String(value ?? "").trim();

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
  draft: "neutral",
});

/**
 * External authorities, as a reader names them.
 *
 * Batch resolve answers an external ref with
 * `{kind: "external", authority, native_id, title, url, status}`. The chip
 * labels it by its authority — "GitHub", not "external" — because that is the
 * thing the reader recognises and the thing that tells them the status word
 * belongs to someone else's workflow.
 */
const AUTHORITY_LABELS = Object.freeze({
  github: "GitHub",
  multica: "Multica",
  git: "Git",
  ssh_git: "Git",
});

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
      // External refs: `kind: "external"` with the authority that owns the
      // record, the id it knows it by, and that system's own status word.
      authority: asText(row?.authority ?? row?.source?.authority),
      nativeId: asText(row?.native_id ?? row?.source?.native_id),
      // `owner` is an actor ref; `owner_display` is the name a reader knows it
      // by, and core falls back to the ref when it cannot resolve a name.
      ownerDisplay: asText(row?.owner_display) || asText(row?.owner),
      // `board` and `next_step` are objects in the contract. Board metadata
      // needs independent board visibility, so it can be absent on a ref the
      // reader can otherwise see.
      board: asText(row?.board?.title ?? row?.board?.ref ?? row?.board),
      boardRef: asText(row?.board?.ref),
      priority: asText(row?.priority),
      lastMovedAt: asText(row?.last_moved_at),
      nextStep: asText(row?.next_step?.title ?? row?.next_step),
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

  const authority = asText(hit?.authority) || external?.source || "";
  // `kind: "external"` says "this record lives somewhere else"; the authority
  // says where. Both are more use to a reader than the word "external".
  const isExternalKind = asText(hit?.kind) === "external";

  // For a work URL the URL itself is the more specific answer: core resolves
  // an external link through a source-backed card and so calls it a `card`,
  // but "PR" is what the reader recognises, and it is read off the URL rather
  // than guessed.
  const kind =
    external?.kind || (isExternalKind ? "" : asText(hit?.kind)) || prefix;

  /*
   * A valid external ref is never "not found".
   *
   * A GitHub pull request the browser can read off the URL, or one core
   * answered with an `external` row, is a real destination — so a resolve that
   * came back empty or failed must not turn a working link into a dashed
   * "not found" chip. That was the plan-graph bug: every external step read
   * "not found" while linking perfectly well.
   */
  const externalIdentity = Boolean(external) || isExternalKind;
  const resolvable = externalIdentity
    ? true
    : hit
      ? hit.resolvable !== false
      : false;

  const title =
    asText(hit?.title) ||
    external?.label ||
    asText(hit?.nativeId) ||
    (resolvable ? raw : "");

  const status = asText(hit?.status);
  const authorityLabel = AUTHORITY_LABELS[authority] ?? "";
  return {
    raw,
    kind,
    // The most specific thing known: "PR" when the kind says so, otherwise the
    // authority that owns the record. An external ref always says one or the
    // other, so a chip never reads "external" at the reader.
    kindLabel: externalIdentity
      ? KIND_LABELS[kind] || authorityLabel || "External"
      : (KIND_LABELS[kind] ?? ""),
    title: title || raw,
    status,
    statusLabel: status ? status.replaceAll("_", " ") : "",
    statusTone: STATUS_TONES[status] ?? "neutral",
    owner: asText(hit?.ownerDisplay) || asText(hit?.owner),
    priority: asText(hit?.priority),
    board: asText(hit?.board),
    nextStep: asText(hit?.nextStep),
    lastMovedAt: asText(hit?.lastMovedAt),
    progress: hit?.progress ?? null,
    resolvable,
    isExternal: externalIdentity,
    authority,
    authorityLabel,
    nativeId: asText(hit?.nativeId),
    href: refHref({
      raw,
      prefix,
      value,
      hit,
      external,
      context,
      resolvable,
      externalIdentity,
    }),
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

function refHref({
  raw,
  prefix,
  value,
  hit,
  external,
  context,
  resolvable,
  externalIdentity = false,
}) {
  // Nothing to open: a "not found" chip must not offer a link into a 404.
  if (!resolvable) return "";
  if (external) return safeRefDestination(raw);
  // An external row core resolved carries the URL on the record.
  if (externalIdentity) return safeRefDestination(hit?.url);

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

/**
 * Resolve any number of refs within the contract's per-request maximum.
 *
 * `/refs/resolve` takes at most 200 refs, and a valid report can name more. One
 * oversized request is rejected whole, which turned every chip on the page into
 * "not found"; batching keeps a long report working and keeps one slow or
 * failing batch from taking the others down with it.
 *
 * @param {string[]} refs
 * @param {(batch: string[]) => Promise<object>} resolve the client call
 * @param {{size?: number}} [options]
 * @returns {Promise<Map<string, object>>}
 */
export async function resolveRefsInBatches(
  refs,
  resolve,
  { size = MAX_BATCH_REFS } = {},
) {
  const unique = [...new Set((refs ?? []).map(asText).filter(Boolean))];
  if (!unique.length) return new Map();

  const batches = [];
  for (let index = 0; index < unique.length; index += size) {
    batches.push(unique.slice(index, index + size));
  }

  const settled = await Promise.allSettled(
    batches.map((batch) => resolve(batch)),
  );
  const merged = new Map();
  settled.forEach((result, index) => {
    // A batch that failed still contributes its refs, as unresolvable, so those
    // chips read "not found" instead of vanishing.
    const response = result.status === "fulfilled" ? result.value : {};
    for (const [ref, row] of indexResolvedRefs(response, batches[index])) {
      merged.set(ref, row);
    }
  });
  return merged;
}
