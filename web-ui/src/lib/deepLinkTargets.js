const HIGHLIGHT_CLASS = "anx-deep-link-target";
const HIGHLIGHT_MS = 2200;

function asText(value) {
  return String(value ?? "").trim();
}

function pathSegment(value) {
  return encodeURIComponent(String(value));
}

function splitTypedRef(value) {
  const text = asText(value);
  const idx = text.indexOf(":");
  if (idx <= 0) {
    return { prefix: "", value: "" };
  }
  return {
    prefix: text.slice(0, idx).trim(),
    value: text.slice(idx + 1).trim(),
  };
}

function firstRefValue(refs, prefix) {
  for (const ref of Array.isArray(refs) ? refs : []) {
    const parsed = splitTypedRef(ref);
    if (parsed.prefix === prefix && parsed.value) {
      return parsed.value;
    }
  }
  return "";
}

function decodeFragmentValue(value) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export function parseDeepLinkHash(hash) {
  const raw = asText(hash).replace(/^#/, "");
  if (!raw) {
    return { kind: "", id: "", legacy: false };
  }

  if (raw.startsWith("message-")) {
    return {
      kind: "message",
      id: decodeFragmentValue(raw.slice("message-".length)),
      legacy: false,
    };
  }

  if (raw.startsWith("event-")) {
    return {
      kind: "event",
      id: decodeFragmentValue(raw.slice("event-".length)),
      legacy: false,
    };
  }

  return { kind: "event", id: decodeFragmentValue(raw), legacy: true };
}

export function messageTargetFromHash(hash) {
  const target = parseDeepLinkHash(hash);
  if (target.kind === "message") {
    return target;
  }
  if (target.kind === "event" && target.id) {
    return { ...target, kind: "message", legacy: true };
  }
  return { kind: "", id: "", legacy: false };
}

export function timelineTargetFromHash(hash) {
  const target = parseDeepLinkHash(hash);
  if (target.kind === "event" && target.id) {
    return target;
  }
  return { kind: "", id: "", legacy: false };
}

export function findVerticalScrollport(startEl) {
  for (
    let el = startEl instanceof HTMLElement ? startEl : null;
    el;
    el = el.parentElement
  ) {
    const style = getComputedStyle(el);
    const oy = style.overflowY;
    if (
      (oy === "auto" || oy === "scroll") &&
      el.scrollHeight > el.clientHeight + 1
    ) {
      return el;
    }
  }
  return null;
}

export function scrollAndHighlightTarget(element, options = {}) {
  if (!(element instanceof HTMLElement)) {
    return;
  }

  const scrollport =
    options.scrollport instanceof HTMLElement
      ? options.scrollport
      : findVerticalScrollport(element.parentElement);

  if (scrollport) {
    const elementRect = element.getBoundingClientRect();
    const portRect = scrollport.getBoundingClientRect();
    const delta =
      elementRect.top - portRect.top - Number(options.offsetTop ?? 12);
    const maxTop = Math.max(
      0,
      scrollport.scrollHeight - scrollport.clientHeight,
    );
    scrollport.scrollTo({
      top: Math.min(maxTop, Math.max(0, scrollport.scrollTop + delta)),
      behavior: options.behavior || "smooth",
    });
  } else {
    element.scrollIntoView({
      behavior: options.behavior || "smooth",
      block: options.block || "center",
    });
  }

  element.classList.remove(HIGHLIGHT_CLASS);
  void element.offsetWidth;
  element.classList.add(HIGHLIGHT_CLASS);
  window.setTimeout(() => {
    element.classList.remove(HIGHLIGHT_CLASS);
  }, HIGHLIGHT_MS);
}

const eventRouteCache = new Map();

export async function eventRouteForRef(eventId, client) {
  const id = asText(eventId);
  if (!id || !client || typeof client.getEvent !== "function") {
    return null;
  }
  if (!eventRouteCache.has(id)) {
    eventRouteCache.set(
      id,
      client
        .getEvent(id)
        .then((result) => result?.event ?? null)
        .catch(() => null),
    );
  }
  return eventRouteCache.get(id);
}

/**
 * Shared internal event/message href policy. Use these helpers instead of
 * hand-building `#event-...` URLs so links land on the right tab and element.
 */
export function messageEventHref({
  eventId,
  threadId = "",
  topicId = "",
  workspaceHref,
} = {}) {
  const id = asText(eventId);
  if (!id || typeof workspaceHref !== "function") {
    return "";
  }
  const topic = asText(topicId);
  if (topic) {
    return workspaceHref(
      `/topics/${pathSegment(topic)}?tab=messages#message-${pathSegment(id)}`,
    );
  }
  const thread = asText(threadId);
  if (thread) {
    return workspaceHref(
      `/threads/${pathSegment(thread)}?tab=messages#message-${pathSegment(id)}`,
    );
  }
  return workspaceHref(`/events#${pathSegment(id)}`);
}

export function threadTimelineEventHref({
  eventId,
  threadId = "",
  workspaceHref,
} = {}) {
  const id = asText(eventId);
  const thread = asText(threadId);
  if (!id || !thread || typeof workspaceHref !== "function") {
    return "";
  }
  return workspaceHref(
    `/threads/${pathSegment(thread)}?tab=timeline#event-${pathSegment(id)}`,
  );
}

export function messageEventHrefFromEvent(event, { workspaceHref } = {}) {
  const eventId = asText(event?.id);
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  const topicFromRef = splitTypedRef(event?.topic_ref);
  const threadFromRef = splitTypedRef(event?.thread_ref);
  return messageEventHref({
    eventId,
    workspaceHref,
    topicId:
      (topicFromRef.prefix === "topic" ? topicFromRef.value : "") ||
      asText(event?.topic_id || event?.topicId) ||
      firstRefValue(refs, "topic"),
    threadId:
      asText(event?.thread_id || event?.threadId) ||
      (threadFromRef.prefix === "thread" ? threadFromRef.value : "") ||
      firstRefValue(refs, "thread"),
  });
}
