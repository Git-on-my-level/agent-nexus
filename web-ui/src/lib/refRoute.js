/**
 * The universal ref link: `/o/<org>/w/<ws>/r/<ref>`.
 *
 * Agents hand a human one browser link for any typed ref. This module turns
 * that ref into the page that shows it, so the route itself only waits for a
 * session and navigates. Everything here is plain JavaScript and testable
 * without SvelteKit.
 *
 * Two rules shape it:
 *
 * - **Same origin, same workspace.** A destination is always an app path built
 *   here from the ref, with the value encoded as one path segment or one query
 *   value, then confined to the workspace prefix. A `url:` ref, an external
 *   record, or a `url` field from core is never followed.
 * - **Read before going.** Each ref is read through the same core route its
 *   destination page uses, so a ref that does not exist or that the reader
 *   cannot see lands on one friendly "couldn't find" page instead of a broken
 *   destination. A transient failure is reported as retryable, not as missing.
 */

import { messageEventHrefFromEvent } from "./deepLinkTargets.js";
import { resourceRouteSegment } from "./resourceIdentity.js";
import { parseRef } from "./typedRefs.js";
import { workspacePath } from "./workspacePaths.js";

/** Accepted prefixes, mapped to the resource kind they name. */
export const REF_ROUTE_KINDS = Object.freeze({
  card: "card",
  doc: "document",
  document: "document",
  document_revision: "document_revision",
  topic: "topic",
  board: "board",
  thread: "thread",
  inbox: "inbox",
  event: "event",
  actor: "actor",
  agent: "actor",
  artifact: "artifact",
  url: "url",
});

/** Event type an `anx ask` writes; its page is the decision. */
const ASK_EVENT_TYPE = "human_attention_requested";

const MAX_REF_LENGTH = 512;
const PREFIX_RE = /^[a-z][a-z_]*$/;
/*
 * Handles, ids and inbox item ids (which carry colons). Nothing that can act
 * as a separator, a query, a fragment or an escape in the path we build.
 */
const VALUE_RE = /^[A-Za-z0-9._~:@+=-]+$/;

const asText = (value) => String(value ?? "").trim();
const segment = (value) => encodeURIComponent(String(value));
const identityHref = (path) => path;

function hasControlCharacter(value) {
  for (let i = 0; i < value.length; i += 1) {
    const code = value.charCodeAt(i);
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

/** A value safe to place in one path segment or query value. */
export function isSafeRefValue(value) {
  const text = String(value ?? "");
  if (!text || text.length > MAX_REF_LENGTH) return false;
  // `.` and `..` survive encodeURIComponent and are dot segments.
  if (/^\.+$/.test(text)) return false;
  return VALUE_RE.test(text);
}

/**
 * Parse the route parameter into a typed ref.
 *
 * @param {unknown} raw the decoded `[ref]` route parameter
 * @returns {{ ok: boolean, ref: string, prefix: string, kind: string, value: string, reason?: string }}
 */
export function parseRouteRef(raw) {
  const ref = asText(raw);
  const fail = (reason, extra = {}) => ({
    ok: false,
    ref,
    prefix: "",
    kind: "",
    value: "",
    reason,
    ...extra,
  });
  if (!ref || ref.length > MAX_REF_LENGTH || hasControlCharacter(ref)) {
    return fail("malformed");
  }
  const { prefix, value } = parseRef(ref);
  if (!prefix || !PREFIX_RE.test(prefix) || !asText(value)) {
    return fail("malformed");
  }
  const kind = REF_ROUTE_KINDS[prefix] ?? "";
  if (!kind) return fail("unknown_type", { prefix });
  // An external link is not a workspace page; its value is a URL and is never
  // validated as a path segment, because it is never placed in one.
  if (kind === "url") {
    return { ok: true, ref, prefix, kind, value: asText(value) };
  }
  if (!isSafeRefValue(value)) return fail("malformed", { prefix });
  return { ok: true, ref, prefix, kind, value };
}

/**
 * Is this a workspace-relative app path we are willing to navigate to?
 * One leading slash, no backslash, no control characters, no dot segments.
 */
export function isSafeAppPath(path) {
  const text = String(path ?? "");
  if (!text.startsWith("/") || text.startsWith("//")) return false;
  if (text.includes("\\") || hasControlCharacter(text)) return false;
  const pathname = text.split(/[?#]/, 1)[0];
  return !pathname
    .split("/")
    .some((part) => part === "." || part === ".." || /^%2e/i.test(part));
}

/**
 * The browser destination for a workspace-relative app path, confined to the
 * workspace. Returns "" when the path would leave it.
 *
 * @param {string} organizationSlug
 * @param {string} workspaceSlug
 * @param {string} path
 * @param {{ origin?: string, basePath?: string }} [options]
 */
export function refRouteHref(
  organizationSlug,
  workspaceSlug,
  path,
  { origin = "https://workspace.invalid", basePath } = {},
) {
  if (!isSafeAppPath(path)) return "";
  try {
    // An omitted `basePath` falls through to the app's own base.
    const prefix = workspacePath(
      organizationSlug,
      workspaceSlug,
      "/",
      basePath,
    );
    const target = workspacePath(
      organizationSlug,
      workspaceSlug,
      path,
      basePath,
    );
    const base = new URL(origin);
    const resolved = new URL(target, base);
    if (resolved.origin !== base.origin) return "";
    if (!resolved.pathname.startsWith(`${prefix}/`)) return "";
    return `${resolved.pathname}${resolved.search}${resolved.hash}`;
  } catch {
    return "";
  }
}

/**
 * A core failure that means "this ref is not there for you": missing, not
 * visible, or rejected as a ref. Anything else (network, 5xx, 401, 429) is
 * transient and the reader should be offered a retry.
 */
export function isNotFoundFailure(error) {
  const status = Number(error?.status ?? error?.coreHttpStatus);
  return [400, 403, 404, 410, 422].includes(status);
}

const notFound = (ref, reason = "not_found") => ({
  status: "not_found",
  ref,
  reason,
});
const noPage = (ref, kind, title = "") => ({
  status: "no_page",
  ref,
  kind,
  title: asText(title),
});
const redirectTo = (ref, path) =>
  isSafeAppPath(path) ? { status: "redirect", ref, path } : notFound(ref);

/** The safe route segment for a record, falling back to the ref's own value. */
function routeSegment(record, kind, fallback) {
  const candidate = resourceRouteSegment(record, kind);
  return isSafeRefValue(candidate) ? candidate : fallback;
}

function eventPath(event, fallbackId) {
  const id = isSafeRefValue(asText(event?.id)) ? asText(event.id) : fallbackId;
  if (asText(event?.type) === ASK_EVENT_TYPE) {
    return `/decide/${segment(`event:${id}`)}`;
  }
  const threadId = asText(event?.thread_id || event?.threadId);
  const safeThread = isSafeRefValue(threadId) ? threadId : "";
  if (asText(event?.type) === "message_posted") {
    return messageEventHrefFromEvent(
      { ...event, id, thread_id: safeThread, thread_ref: "", refs: [] },
      { workspaceHref: identityHref },
    );
  }
  if (safeThread) {
    return `/threads/${segment(safeThread)}?tab=timeline#event-${segment(id)}`;
  }
  return `/events#${segment(id)}`;
}

function artifactPath(artifact) {
  const owner = parseRef(asText(artifact?.owner_ref));
  const documentId =
    asText(artifact?.document_id) ||
    (owner.prefix === "document" ? asText(owner.value) : "");
  if (isSafeRefValue(documentId)) return `/docs/${segment(documentId)}`;
  const cardId =
    asText(artifact?.card_id) ||
    (owner.prefix === "card" ? asText(owner.value) : "");
  if (isSafeRefValue(cardId)) {
    return `/initiatives/${segment(`card:${cardId}`)}`;
  }
  return "";
}

/**
 * Where a ref should open.
 *
 * @param {unknown} raw the decoded route parameter
 * @param {Record<string, Function>} client the workspace core client
 * @returns {Promise<
 *   | { status: "redirect", ref: string, path: string }
 *   | { status: "not_found", ref: string, reason: string }
 *   | { status: "no_page", ref: string, kind: string, title: string }
 *   | { status: "error", ref: string, error: unknown }
 * >}
 */
export async function resolveRefRoute(raw, client) {
  const parsed = parseRouteRef(raw);
  const { ref, kind, value } = parsed;
  if (!parsed.ok) return notFound(ref, parsed.reason);

  try {
    switch (kind) {
      case "card": {
        const { work } = (await client.getWork(`card:${value}`)) ?? {};
        if (!work) return notFound(ref);
        const id = routeSegment(work, "card", value);
        return redirectTo(ref, `/initiatives/${segment(`card:${id}`)}`);
      }
      case "document": {
        const { document } = (await client.getDocument(value)) ?? {};
        if (!document) return notFound(ref);
        return redirectTo(
          ref,
          `/docs/${segment(routeSegment(document, "document", value))}`,
        );
      }
      case "document_revision":
        // The revision page finds its document and reports its own misses.
        return redirectTo(ref, `/docs/revisions/${segment(value)}`);
      case "topic": {
        const { topic } = (await client.getTopic(value)) ?? {};
        if (!topic) return notFound(ref);
        const id = routeSegment(topic, "topic", value);
        return redirectTo(
          ref,
          `/initiatives?project_ref=${segment(`topic:${id}`)}`,
        );
      }
      case "board": {
        const { board } = (await client.getBoard(value)) ?? {};
        if (!board) return notFound(ref);
        return noPage(ref, kind, board.title);
      }
      case "thread": {
        const { thread } = (await client.getThread(value)) ?? {};
        if (!thread) return notFound(ref);
        return redirectTo(
          ref,
          `/threads/${segment(routeSegment(thread, "thread", value))}`,
        );
      }
      case "inbox": {
        // Inbox item ids carry their own `inbox:` prefix; core accepts both.
        const { item } = (await client.getInboxItem(ref)) ?? {};
        if (!item) return notFound(ref);
        const id = isSafeRefValue(asText(item.id)) ? asText(item.id) : ref;
        return redirectTo(ref, `/decide/${segment(id)}`);
      }
      case "event": {
        const { event } = (await client.getEvent(value)) ?? {};
        if (!event) return notFound(ref);
        return redirectTo(ref, eventPath(event, value));
      }
      case "actor": {
        const { agent } = (await client.getAgent(value)) ?? {};
        if (!agent) return notFound(ref);
        const id = isSafeRefValue(asText(agent.id)) ? asText(agent.id) : value;
        return redirectTo(ref, `/agents/${segment(id)}`);
      }
      case "artifact": {
        const { artifact } = (await client.getArtifact(value)) ?? {};
        if (!artifact) return notFound(ref);
        const path = artifactPath(artifact);
        return path
          ? redirectTo(ref, path)
          : noPage(
              ref,
              kind,
              artifact.original_filename || artifact.summary || "",
            );
      }
      case "url":
        return noPage(ref, kind);
      default:
        return notFound(ref, "unknown_type");
    }
  } catch (error) {
    if (isNotFoundFailure(error)) return notFound(ref);
    return { status: "error", ref, error };
  }
}
