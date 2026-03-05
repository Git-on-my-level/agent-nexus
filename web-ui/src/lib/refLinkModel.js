import { parseRef, renderRef } from "./typedRefs.js";

function asPathSegment(value) {
  return encodeURIComponent(String(value));
}

export function resolveRefLink(refValue, options = {}) {
  const parsed = parseRef(refValue);
  const raw = renderRef(parsed);
  const prefix = parsed.prefix;
  const value = parsed.value;
  const threadId = options.threadId;
  const snapshotIsThread = Boolean(options.snapshotIsThread);

  if (!prefix) {
    return {
      raw,
      prefix,
      value,
      kind: "raw",
      label: raw,
      href: "",
      isExternal: false,
      isLink: false,
    };
  }

  if (prefix === "artifact") {
    return {
      raw,
      prefix,
      value,
      kind: "artifact",
      label: raw,
      href: `/artifacts/${asPathSegment(value)}`,
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
      label: raw,
      href: `/threads/${asPathSegment(value)}`,
      isExternal: false,
      isLink: true,
    };
  }

  if (prefix === "snapshot") {
    const href = snapshotIsThread
      ? `/threads/${asPathSegment(value)}`
      : `/snapshots/${asPathSegment(value)}`;
    return {
      raw,
      prefix,
      value,
      kind: "snapshot",
      label: raw,
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
        label: raw,
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
      label: raw,
      href: `/threads/${asPathSegment(threadId)}#event-${asPathSegment(value)}`,
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
      label: raw,
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
      label: raw,
      href: `/inbox#inbox-${asPathSegment(value)}`,
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
    href: "",
    isExternal: false,
    isLink: false,
  };
}
