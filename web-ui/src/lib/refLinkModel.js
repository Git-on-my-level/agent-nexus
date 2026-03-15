import { parseRef, renderRef } from "./typedRefs.js";
import { appPath, projectPath } from "./projectPaths.js";

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

function humanizedLabelForPrefix(prefix, value) {
  if (prefix === "artifact") return "Artifact";
  if (prefix === "thread") return "Thread";
  if (prefix === "snapshot") return "Snapshot";
  if (prefix === "event") return "Event";
  if (prefix === "document") return `Document ${value}`.trim();
  if (prefix === "document_revision")
    return `Document revision ${value}`.trim();
  if (prefix === "url") return summarizeUrl(value);
  if (prefix === "inbox") return "Inbox item";
  if (prefix === "board") return `Board ${value}`.trim();
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

function toProjectHref(projectSlug, pathname) {
  return projectSlug ? projectPath(projectSlug, pathname) : appPath(pathname);
}

function buildInternalHref(projectSlug, pathname) {
  return toProjectHref(projectSlug, pathname);
}

const LINK_RESOLVERS = {
  artifact: ({ projectSlug, value }) =>
    buildInternalHref(projectSlug, `/artifacts/${asPathSegment(value)}`),
  thread: ({ projectSlug, value }) =>
    buildInternalHref(projectSlug, `/threads/${asPathSegment(value)}`),
  snapshot: ({ projectSlug, snapshotIsThread, value }) =>
    snapshotIsThread
      ? buildInternalHref(projectSlug, `/threads/${asPathSegment(value)}`)
      : buildInternalHref(projectSlug, `/snapshots/${asPathSegment(value)}`),
  event: ({ projectSlug, threadId, value }) =>
    threadId
      ? buildInternalHref(
          projectSlug,
          `/threads/${asPathSegment(threadId)}#event-${asPathSegment(value)}`,
        )
      : "",
  url: ({ value }) => value,
  inbox: ({ projectSlug, value }) =>
    buildInternalHref(projectSlug, `/inbox#inbox-${asPathSegment(value)}`),
  document: ({ projectSlug, value }) =>
    buildInternalHref(projectSlug, `/docs/${asPathSegment(value)}`),
  document_revision: ({ projectSlug, value }) =>
    buildInternalHref(projectSlug, `/docs/revisions/${asPathSegment(value)}`),
  board: ({ projectSlug, value }) =>
    buildInternalHref(projectSlug, `/boards/${asPathSegment(value)}`),
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
  const projectSlug = options.projectSlug;
  const threadId = options.threadId;
  const snapshotIsThread = Boolean(options.snapshotIsThread);

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
      href: linkResolver({ projectSlug, snapshotIsThread, threadId, value }),
      isExternal: prefix === "url",
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
