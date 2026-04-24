import { toTimelineViewEvent } from "./timelineUtils.js";

/** @param {unknown} event @param {string} requiredRef */
export function eventRefsInclude(event, requiredRef) {
  const want = String(requiredRef ?? "").trim();
  if (!want) {
    return true;
  }
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  return refs.some((r) => String(r).trim() === want);
}

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

function collectEventRefIds(event) {
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  const ids = [];
  for (const ref of refs) {
    const value = String(ref ?? "").trim();
    if (value.startsWith("event:")) {
      const id = value.slice("event:".length).trim();
      if (id) {
        ids.push(id);
      }
    }
  }
  return ids;
}

/**
 * Parent for a reply is conveyed as `event:<parent_event_id>` in refs (see oar-schema
 * message_posted). Messages may include multiple `event:` refs (e.g. citations); the
 * parent is the ref that points at another message_posted in this thread when possible.
 */
function extractParentEventId(event, messageIdsInThread) {
  const candidates = collectEventRefIds(event);
  if (candidates.length === 0) {
    return "";
  }
  const idSet =
    messageIdsInThread instanceof Set
      ? messageIdsInThread
      : new Set(messageIdsInThread ?? []);
  for (const id of candidates) {
    if (idSet.has(id)) {
      return id;
    }
  }
  return candidates[0];
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
  const messageIdsInThread = options.messageIdsInThread;
  const parentEventId = extractParentEventId(event, messageIdsInThread);
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

function wouldCreateMessageParentCycle(childId, parentId, nodesById) {
  const child = String(childId ?? "").trim();
  let cur = String(parentId ?? "").trim();
  if (!child || !cur || child === cur) {
    return true;
  }
  const seen = new Set();
  while (cur) {
    if (cur === child) {
      return true;
    }
    if (seen.has(cur)) {
      return true;
    }
    seen.add(cur);
    const n = nodesById.get(cur);
    cur = n?.parentEventId ? String(n.parentEventId).trim() : "";
  }
  return false;
}

export function toMessageThreadView(events = [], options = {}) {
  const rawMessages = Array.isArray(events)
    ? events.filter((event) => String(event?.type ?? "") === "message_posted")
    : [];
  const messageIdsInThread = new Set(
    rawMessages.map((e) => String(e?.id ?? "").trim()).filter(Boolean),
  );
  const messages = rawMessages
    .map((event) =>
      decorateMessageEvent(event, { ...options, messageIdsInThread }),
    )
    .sort(compareEventsOldestFirst);

  const nodesById = new Map(
    messages.map((message) => [message.id, { ...message, children: [] }]),
  );
  const roots = [];

  for (const message of messages) {
    const node = nodesById.get(message.id);
    const parentNode = message.parentEventId
      ? nodesById.get(message.parentEventId)
      : null;
    if (
      parentNode &&
      !wouldCreateMessageParentCycle(
        message.id,
        message.parentEventId,
        nodesById,
      )
    ) {
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
  roots.sort(compareEventsOldestFirst);

  return roots;
}

export function flattenMessageThreadView(threads = []) {
  const out = [];
  const seenIds = new Set();

  function visit(nodes) {
    for (const node of nodes) {
      const id = String(node?.id ?? "").trim();
      if (id) {
        if (seenIds.has(id)) {
          continue;
        }
        seenIds.add(id);
      }
      out.push(node);
      if (Array.isArray(node.children) && node.children.length > 0) {
        visit(node.children);
      }
    }
  }

  visit(Array.isArray(threads) ? threads : []);
  return out;
}
