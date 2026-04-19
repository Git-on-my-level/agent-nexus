import { parseRef, renderRef } from "./typedRefs.js";
import { workspacePath } from "./workspacePaths.js";

function asPathSegment(value) {
  return encodeURIComponent(String(value));
}

function lookupLabelHint(raw, prefix, value, labelHints) {
  if (!labelHints || typeof labelHints !== "object") {
    return "";
  }

  const direct =
    labelHints[raw] ?? labelHints[`${prefix}:${value}`] ?? labelHints[value];
  return String(direct ?? "").trim();
}

function summarizeUrl(value) {
  try {
    const url = new URL(String(value));
    const path = String(url.pathname ?? "").replace(/\/+$/, "") || "/";
    const shownPath = path.length > 28 ? `${path.slice(0, 28)}...` : path;
    return `${url.hostname}${shownPath}`;
  } catch {
    return "External link";
  }
}

function shouldHumanizeByDefault(prefix) {
  return prefix === "document" || prefix === "document_revision";
}

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function compactValue(value) {
  if (UUID_RE.test(value)) return value.slice(0, 10);
  return value;
}

function humanizedLabelForPrefix(prefix, value) {
  const short = compactValue(value);
  if (prefix === "artifact") return `Artifact ${short}`.trim();
  if (prefix === "card") return `Card ${short}`.trim();
  if (prefix === "thread") return `Thread ${short}`.trim();
  if (prefix === "topic") return `Topic ${short}`.trim();
  if (prefix === "event") return "Event";
  if (prefix === "document") return `Document ${short}`.trim();
  if (prefix === "document_revision")
    return `Document revision ${short}`.trim();
  if (prefix === "url") return summarizeUrl(value);
  if (prefix === "inbox") return "Inbox item";
  if (prefix === "board") return `Board ${short}`.trim();
  return "";
}

function resolveRefLabels(raw, prefix, value, options = {}) {
  const humanize = Boolean(options.humanize) || shouldHumanizeByDefault(prefix);
  const labelHint = lookupLabelHint(raw, prefix, value, options.labelHints);

  if (!humanize) {
    return {
      label: raw,
      primaryLabel: raw,
      secondaryLabel: "",
    };
  }

  const primaryLabel =
    labelHint || humanizedLabelForPrefix(prefix, value) || raw;
  const secondaryLabel = primaryLabel === raw ? "" : raw;
  return {
    label: primaryLabel,
    primaryLabel,
    secondaryLabel,
  };
}

function toWorkspaceHref(organizationSlug, workspaceSlug, pathname) {
  const org = String(organizationSlug ?? "").trim();
  const ws = String(workspaceSlug ?? "").trim();
  if (!org || !ws) {
    return "";
  }
  return workspacePath(org, ws, pathname);
}

function buildInternalHref(workspaceSlug, pathname, organizationSlug) {
  return toWorkspaceHref(organizationSlug, workspaceSlug, pathname);
}

const LINK_RESOLVERS = {
  artifact: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/artifacts/${asPathSegment(value)}`,
      organizationSlug,
    ),
  thread: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/threads/${asPathSegment(value)}`,
      organizationSlug,
    ),
  topic: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/topics/${asPathSegment(value)}`,
      organizationSlug,
    ),
  card: ({ workspaceSlug, organizationSlug, boardId, value }) =>
    boardId
      ? buildInternalHref(
          workspaceSlug,
          `/boards/${asPathSegment(boardId)}?card=${asPathSegment(value)}`,
          organizationSlug,
        )
      : "",
  event: ({ workspaceSlug, organizationSlug, threadId, value }) =>
    threadId
      ? buildInternalHref(
          workspaceSlug,
          `/threads/${asPathSegment(threadId)}#event-${asPathSegment(value)}`,
          organizationSlug,
        )
      : "",
  url: ({ value }) => value,
  inbox: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/inbox#inbox-${asPathSegment(value)}`,
      organizationSlug,
    ),
  document: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/docs/${asPathSegment(value)}`,
      organizationSlug,
    ),
  document_revision: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/docs/revisions/${asPathSegment(value)}`,
      organizationSlug,
    ),
  board: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/boards/${asPathSegment(value)}`,
      organizationSlug,
    ),
};

function createResolvedLink(raw, prefix, value, labels, { href, isExternal }) {
  return {
    raw,
    prefix,
    value,
    kind: prefix,
    ...labels,
    href,
    isExternal,
    isLink: Boolean(href),
  };
}

export function resolveRefLink(refValue, options = {}) {
  const parsed = parseRef(refValue);
  const raw = renderRef(parsed);
  const prefix = parsed.prefix;
  const value = parsed.value;
  const workspaceSlug = options.workspaceSlug;
  const organizationSlug = String(options.organizationSlug ?? "").trim();
  const boardId = options.boardId;
  const threadId = options.threadId;

  if (!prefix) {
    return {
      raw,
      prefix,
      value,
      kind: "raw",
      ...resolveRefLabels(raw, prefix, value, options),
      href: "",
      isExternal: false,
      isLink: false,
    };
  }

  const labels = resolveRefLabels(raw, prefix, value, options);
  const linkResolver = LINK_RESOLVERS[prefix];
  if (linkResolver) {
    return createResolvedLink(raw, prefix, value, labels, {
      href: linkResolver({
        workspaceSlug,
        organizationSlug,
        threadId,
        boardId,
        value,
      }),
      isExternal: prefix === "url",
      boardId,
    });
  }

  return {
    raw,
    prefix,
    value,
    kind: "unknown",
    label: raw,
    primaryLabel: raw,
    secondaryLabel: "",
    href: "",
    isExternal: false,
    isLink: false,
  };
}
