import { toTimelineViewEvent } from "./timelineUtils.js";

function parseEventTimeMs(event) {
  const ts = event?.ts;
  if (ts == null || ts === "") {
    return Number.NEGATIVE_INFINITY;
  }
  const ms = Date.parse(String(ts));
  return Number.isFinite(ms) ? ms : Number.NEGATIVE_INFINITY;
}

function compareEventsOldestFirst(a, b) {
  const ta = parseEventTimeMs(a);
  const tb = parseEventTimeMs(b);
  if (ta !== tb) {
    return ta - tb;
  }
  return String(a.id ?? "").localeCompare(String(b.id ?? ""));
}

function compareEventsNewestFirst(a, b) {
  const tb = parseEventTimeMs(b);
  const ta = parseEventTimeMs(a);
  if (tb !== ta) {
    return tb - ta;
  }
  return String(b.id ?? "").localeCompare(String(a.id ?? ""));
}

function extractParentEventId(event) {
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  for (const ref of refs) {
    const value = String(ref ?? "");
    if (value.startsWith("event:")) {
      return value.slice("event:".length);
    }
  }
  return "";
}

function stripMessagePrefix(value) {
  const text = String(value ?? "").trim();
  if (text.startsWith("Message: ")) {
    return text.slice("Message: ".length).trim();
  }
  return text;
}

function extractMessageText(event) {
  const payloadText =
    typeof event?.payload?.text === "string" ? event.payload.text.trim() : "";
  if (payloadText) {
    return payloadText;
  }
  return stripMessagePrefix(event?.summary);
}

function decorateMessageEvent(event, options = {}) {
  const view = toTimelineViewEvent(event, options);
  const parentEventId = extractParentEventId(event);
  const threadId = String(options.threadId ?? event?.thread_id ?? "").trim();

  return {
    ...view,
    parentEventId,
    messageText: extractMessageText(event),
    displayRefs: view.refs.filter((refValue) => {
      const ref = String(refValue ?? "");
      if (threadId && ref === `thread:${threadId}`) {
        return false;
      }
      if (parentEventId && ref === `event:${parentEventId}`) {
        return false;
      }
      return true;
    }),
  };
}

export function toMessageThreadView(events = [], options = {}) {
  const messages = Array.isArray(events)
    ? events
        .filter((event) => String(event?.type ?? "") === "message_posted")
        .map((event) => decorateMessageEvent(event, options))
        .sort(compareEventsOldestFirst)
    : [];

  const nodesById = new Map(
    messages.map((message) => [message.id, { ...message, children: [] }]),
  );
  const roots = [];

  for (const message of messages) {
    const node = nodesById.get(message.id);
    const parentNode = message.parentEventId
      ? nodesById.get(message.parentEventId)
      : null;
    if (parentNode) {
      parentNode.children.push(node);
      continue;
    }
    roots.push(node);
  }

  function sortChildren(node) {
    node.children.sort(compareEventsOldestFirst);
    for (const child of node.children) {
      sortChildren(child);
    }
  }

  for (const root of roots) {
    sortChildren(root);
  }
  roots.sort(compareEventsNewestFirst);

  return roots;
}

export function flattenMessageThreadView(threads = []) {
  const out = [];

  function visit(nodes) {
    for (const node of nodes) {
      out.push(node);
      if (Array.isArray(node.children) && node.children.length > 0) {
        visit(node.children);
      }
    }
  }

  visit(Array.isArray(threads) ? threads : []);
  return out;
}
