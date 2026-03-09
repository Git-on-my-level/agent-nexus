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

function humanizedLabelForPrefix(prefix, value) {
  if (prefix === "artifact") return "Artifact";
  if (prefix === "thread") return "Thread";
  if (prefix === "snapshot") return "Snapshot";
  if (prefix === "event") return "Event";
  if (prefix === "url") return summarizeUrl(value);
  if (prefix === "inbox") return "Inbox item";
  return "";
}

function resolveRefLabels(raw, prefix, value, options = {}) {
  const humanize = Boolean(options.humanize);
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

  if (prefix === "artifact") {
    return {
      raw,
      prefix,
      value,
      kind: "artifact",
      ...labels,
      href: toProjectHref(projectSlug, `/artifacts/${asPathSegment(value)}`),
      isExternal: false,
      isLink: true,
    };
  }

  if (prefix === "thread") {
    return {
      raw,
      prefix,
      value,
      kind: "thread",
      ...labels,
      href: toProjectHref(projectSlug, `/threads/${asPathSegment(value)}`),
      isExternal: false,
      isLink: true,
    };
  }

  if (prefix === "snapshot") {
    const href = snapshotIsThread
      ? toProjectHref(projectSlug, `/threads/${asPathSegment(value)}`)
      : toProjectHref(projectSlug, `/snapshots/${asPathSegment(value)}`);
    return {
      raw,
      prefix,
      value,
      kind: "snapshot",
      ...labels,
      href,
      isExternal: false,
      isLink: true,
    };
  }

  if (prefix === "event") {
    if (!threadId) {
      return {
        raw,
        prefix,
        value,
        kind: "event",
        ...labels,
        href: "",
        isExternal: false,
        isLink: false,
      };
    }
    return {
      raw,
      prefix,
      value,
      kind: "event",
      ...labels,
      href: toProjectHref(
        projectSlug,
        `/threads/${asPathSegment(threadId)}#event-${asPathSegment(value)}`,
      ),
      isExternal: false,
      isLink: true,
    };
  }

  if (prefix === "url") {
    return {
      raw,
      prefix,
      value,
      kind: "url",
      ...labels,
      href: value,
      isExternal: true,
      isLink: true,
    };
  }

  if (prefix === "inbox") {
    return {
      raw,
      prefix,
      value,
      kind: "inbox",
      ...labels,
      href: toProjectHref(projectSlug, `/inbox#inbox-${asPathSegment(value)}`),
      isExternal: false,
      isLink: true,
    };
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
