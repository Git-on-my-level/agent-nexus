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

/** @param {unknown} message */
function messageIdentityKeys(message) {
  const keys = [];
  const id = String(message?.id ?? "").trim();
  const handle = String(message?.handle ?? "").trim();
  const ref = String(message?.ref ?? "").trim();
  if (id) keys.push(id);
  if (handle) keys.push(handle);
  if (ref.startsWith("event:")) {
    const value = ref.slice("event:".length).trim();
    if (value) keys.push(value);
  }
  return keys;
}

/**
 * Index messages by durable id, public handle, and `event:` ref value so reply
 * parent resolution works after core canonicalizes refs to handles.
 */
export function buildMessageByKey(messages = []) {
  const map = new Map();
  for (const message of Array.isArray(messages) ? messages : []) {
    for (const key of messageIdentityKeys(message)) {
      if (!map.has(key)) {
        map.set(key, message);
      }
    }
  }
  return map;
}

function parentReplyRefValues(parentMessage) {
  const values = new Set();
  const id = String(parentMessage?.id ?? "").trim();
  const handle = String(parentMessage?.handle ?? "").trim();
  const ref = String(parentMessage?.ref ?? "").trim();
  if (id) values.add(id);
  if (handle) values.add(handle);
  if (ref.startsWith("event:")) {
    const value = ref.slice("event:".length).trim();
    if (value) values.add(value);
  }
  return values;
}

function eventRefValue(ref) {
  const text = String(ref ?? "").trim();
  if (!text.startsWith("event:")) return "";
  return text.slice("event:".length).trim();
}

function refIsReplyToParent(ref, parentMessage) {
  if (!parentMessage) return false;
  const value = eventRefValue(ref);
  if (!value) return false;
  return parentReplyRefValues(parentMessage).has(value);
}

/** Hide resolved parent refs and unresolved first reply candidates from chips. */
function shouldHideReplyParentRef(ref, event, parentMessage) {
  if (refIsReplyToParent(ref, parentMessage)) {
    return true;
  }
  if (parentMessage) {
    return false;
  }
  const candidates = collectEventRefIds(event);
  const value = eventRefValue(ref);
  return candidates.length > 0 && value === candidates[0];
}

/**
 * Parent for a reply is conveyed as `event:<parent_event_id>` in refs (see anx-schema
 * message_posted). Core may persist that ref as a public handle while clients keep
 * the durable event id on the message row.
 */
function extractParentEventId(event, messageByKey) {
  const candidates = collectEventRefIds(event);
  if (candidates.length === 0 || !(messageByKey instanceof Map)) {
    return "";
  }
  for (const key of candidates) {
    const parent = messageByKey.get(key);
    if (parent) {
      return String(parent.id ?? "").trim();
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

/**
 * @param {unknown} event
 */
function extractDocumentComment(event) {
  if (String(event?.payload?.kind ?? "") !== "document_text_comment") {
    return null;
  }
  const raw = event?.payload?.document_comment;
  if (!raw || typeof raw !== "object") {
    return null;
  }
  const o = /** @type {Record<string, unknown>} */ (raw);
  return {
    document_id: String(o.document_id ?? "").trim(),
    revision_id: String(o.revision_id ?? "").trim(),
    content_hash: String(o.content_hash ?? "").trim(),
    selected_text: String(o.selected_text ?? "").trim(),
    context_before: String(o.context_before ?? ""),
    context_after: String(o.context_after ?? ""),
    start_offset: typeof o.start_offset === "number" ? o.start_offset : null,
    end_offset: typeof o.end_offset === "number" ? o.end_offset : null,
    anchor_status: String(o.anchor_status ?? "").trim() || "quote_only",
  };
}

function decorateMessageEvent(event, options = {}) {
  const view = toTimelineViewEvent(event, options);
  const messageByKey = options.messageByKey;
  const parentEventId = extractParentEventId(event, messageByKey);
  const parentMessage =
    parentEventId && messageByKey instanceof Map
      ? (messageByKey.get(parentEventId) ?? null)
      : null;
  const threadId = String(options.threadId ?? event?.thread_id ?? "").trim();
  const eventId = String(event?.id ?? "").trim();
  const documentComment = extractDocumentComment(event);
  const suppressDisplayDocumentId = String(
    options.suppressDisplayDocumentId ?? "",
  ).trim();
  const receiptMap =
    options.notificationReceiptsByEventId &&
    typeof options.notificationReceiptsByEventId === "object"
      ? options.notificationReceiptsByEventId
      : {};
  const notificationReceipts = Array.isArray(receiptMap[eventId])
    ? receiptMap[eventId]
    : [];

  // When an event is an anchored document text comment, hide the duplicate
  // `document:<id>` and `document_revision:<id>` ref chips from the rendered
  // message header. The same information is already present in the structured
  // payload (`documentComment.document_id` / `revision_id`) and surfaced
  // visually by the quoted excerpt + anchor status pip in `MessageItem`.
  // Suppressing them here removes ~half the visual height of an anchored
  // comment card without losing any information that agents rely on (the raw
  // refs remain on the underlying event).
  const docCommentDocId = documentComment?.document_id
    ? String(documentComment.document_id).trim()
    : "";
  const docCommentRevisionId = documentComment?.revision_id
    ? String(documentComment.revision_id).trim()
    : "";

  const suppressThreadRefs = Boolean(options.suppressThreadRefs);

  return {
    ...view,
    parentEventId,
    messageText: extractMessageText(event),
    documentComment,
    notificationReceipts,
    displayRefs: view.refs.filter((refValue) => {
      const ref = String(refValue ?? "");
      if (suppressThreadRefs && ref.startsWith("thread:")) {
        return false;
      }
      if (threadId && ref === `thread:${threadId}`) {
        return false;
      }
      if (shouldHideReplyParentRef(ref, event, parentMessage)) {
        return false;
      }
      if (
        documentComment &&
        docCommentDocId &&
        ref === `document:${docCommentDocId}`
      ) {
        return false;
      }
      if (
        documentComment &&
        docCommentRevisionId &&
        ref === `document_revision:${docCommentRevisionId}`
      ) {
        return false;
      }
      if (
        suppressDisplayDocumentId &&
        ref === `document:${suppressDisplayDocumentId}`
      ) {
        return false;
      }
      if (suppressDisplayDocumentId && ref.startsWith("document_revision:")) {
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
  const messageByKey = buildMessageByKey(rawMessages);
  const messages = rawMessages
    .map((event) => decorateMessageEvent(event, { ...options, messageByKey }))
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

function buildReplyToPreview(parentMessage) {
  if (!parentMessage || typeof parentMessage !== "object") {
    return null;
  }
  const id = String(parentMessage.id ?? "").trim();
  if (!id) {
    return null;
  }
  return {
    id,
    authorActorId: String(parentMessage.actor_id ?? "").trim(),
    text: extractMessageText(parentMessage),
    isAnchoredComment: Boolean(extractDocumentComment(parentMessage)),
  };
}

/**
 * Flat, chronological chat stream (oldest first). Each row may include
 * `replyTo` (parent preview).
 */
export function toFlatMessageView(events = [], options = {}) {
  const rawMessages = Array.isArray(events)
    ? events.filter((event) => String(event?.type ?? "") === "message_posted")
    : [];
  const messageByKey = buildMessageByKey(rawMessages);
  const decorateOpts = {
    ...options,
    messageByKey,
    suppressThreadRefs: options.suppressThreadRefs ?? true,
  };
  const messages = rawMessages
    .map((event) => decorateMessageEvent(event, decorateOpts))
    .sort(compareEventsOldestFirst);

  return messages.map((message) => {
    const parent = message.parentEventId
      ? (messageByKey.get(message.parentEventId) ?? null)
      : null;
    return {
      ...message,
      replyTo: buildReplyToPreview(parent),
      children: [],
    };
  });
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
