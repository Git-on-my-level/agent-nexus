import { parseTimestampMs } from "./dateUtils.js";
import {
  readSourceEventTime,
  sortInboxItems,
  splitTypedRef,
} from "./inboxUtils.js";
import {
  buildTimelineRefLabelHints,
  KNOWN_EVENT_TYPES,
} from "./timelineUtils.js";

const HOME_HANDOFF_STORAGE_VERSION = "v1";

const HOME_INCLUDED_EVENT_TYPES = new Set([
  "exception_raised",
  "card_created",
  "card_updated",
  "card_moved",
  "card_resolved",
  "message_posted",
]);

const HOME_EXCLUDED_EVENT_TYPES = new Set([
  "agent_bridge_checked_in",
  "human_attention_requested",
  "human_attention_responded",
]);

/**
 * Prefix order for choosing a single “primary” typed ref on Home (timeline row
 * link + handoff pill filter). More specific resources win; `thread` is a
 * fallback for chat-only events.
 */
export const HOME_TIMELINE_REF_PREFIX_PRIORITY = [
  "topic",
  "document",
  "document_revision",
  "card",
  "board",
  "artifact",
  "thread",
  "inbox",
  "url",
  "event",
];

function toIdRecord(items = []) {
  return Object.fromEntries(
    items
      .map((item) => [String(item?.id ?? "").trim(), item])
      .filter(([id]) => id),
  );
}

function documentBackingThreadIdFromRecord(doc) {
  const threadId = String(doc?.thread_id ?? "").trim();
  if (threadId) {
    return threadId;
  }
  return String(doc?.id ?? "").trim();
}

function documentRevisionMapFromDocuments(documents = []) {
  /** @type {Record<string, { document_id: string, revision_number?: number }>} */
  const revisions = {};
  for (const doc of documents) {
    const docId = String(doc?.id ?? "").trim();
    const hr = doc?.head_revision;
    if (!docId || !hr || typeof hr !== "object") {
      continue;
    }
    const revId = String(hr.revision_id ?? hr.id ?? "").trim();
    if (!revId) {
      continue;
    }
    const revisionNumber = hr.revision_number;
    revisions[revId] = {
      document_id: docId,
      ...(Number.isFinite(Number(revisionNumber))
        ? { revision_number: Number(revisionNumber) }
        : {}),
    };
  }
  return revisions;
}

/**
 * Label hints for Home timeline `RefLink` humanization (topics, boards, docs,
 * artifacts, cards, document threads).
 *
 * Topic `thread:` hints win over document-backed `thread:` hints on collision.
 */
export function buildHomeRefLabelHints({
  topics = [],
  boards = [],
  documents = [],
  artifacts = [],
  cards = [],
} = {}) {
  const docById = toIdRecord(documents);
  const hints = buildTimelineRefLabelHints(
    toIdRecord(artifacts),
    docById,
    documentRevisionMapFromDocuments(documents),
  );

  for (const topic of topics) {
    const topicId = String(topic?.id ?? "").trim();
    const threadId = String(topic?.thread_id ?? "").trim();
    const title = String(topic?.title ?? "").trim();
    if (topicId && title) {
      hints[`topic:${topicId}`] = title;
    }
    if (threadId && title) {
      hints[`thread:${threadId}`] = title;
    }
  }

  for (const doc of documents) {
    const title = String(doc?.title ?? "").trim();
    const backing = documentBackingThreadIdFromRecord(doc);
    if (backing && title && !hints[`thread:${backing}`]) {
      hints[`thread:${backing}`] = title;
    }
  }

  for (const board of boards) {
    const boardId = String(board?.id ?? "").trim();
    const title = String(board?.title ?? "").trim();
    if (boardId && title) {
      hints[`board:${boardId}`] = title;
    }
  }

  for (const card of cards) {
    const cardId = String(card?.id ?? "").trim();
    if (!cardId) {
      continue;
    }
    const title = String(card?.title ?? "").trim();
    hints[`card:${cardId}`] = title || cardId;
  }

  return hints;
}

export function homeTimelinePrimaryRefFromRefs(refs) {
  const list = Array.isArray(refs) ? refs : [];
  for (const prefix of HOME_TIMELINE_REF_PREFIX_PRIORITY) {
    const matched = list.find((r) => splitTypedRef(r).prefix === prefix);
    if (matched) {
      return matched;
    }
  }
  return "";
}

export function homeTimelinePrimaryRefFromEvent(event) {
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  return homeTimelinePrimaryRefFromRefs(refs);
}

function normalizeStorageSegment(value) {
  return String(value ?? "").trim();
}

function timestampToIso(timestampMs) {
  return Number.isFinite(timestampMs)
    ? new Date(timestampMs).toISOString()
    : "";
}

function parseMarkerMs(markerIso) {
  const parsed = parseTimestampMs(markerIso);
  return Number.isFinite(parsed) ? parsed : Number.NaN;
}

function isNewerThanCutoff(value, cutoffMs) {
  if (!Number.isFinite(cutoffMs)) {
    return true;
  }

  const parsed = parseTimestampMs(value);
  return Number.isFinite(parsed) && parsed > cutoffMs;
}

function countDistinct(items = [], cutoffMs, timestampReader) {
  const seen = new Set();
  let count = 0;

  for (const item of items) {
    const id = String(item?.id ?? "").trim();
    if (!id || seen.has(id)) continue;
    if (!isNewerThanCutoff(timestampReader(item), cutoffMs)) continue;
    seen.add(id);
    count += 1;
  }

  return count;
}

function buildThreadToTopicIdMap(topics = []) {
  const map = new Map();
  for (const topic of topics) {
    const threadId = String(topic?.thread_id ?? "").trim();
    const topicId = String(topic?.id ?? "").trim();
    if (threadId && topicId) {
      map.set(threadId, topicId);
    }
  }
  return map;
}

function primaryThreadIdFromEvent(event) {
  const fromField = String(event?.thread_id ?? "").trim();
  if (fromField) {
    return fromField;
  }
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  for (const ref of refs) {
    const { prefix, id } = splitTypedRef(ref);
    if (prefix === "thread" && String(id ?? "").trim()) {
      return String(id).trim();
    }
  }
  return "";
}

/**
 * Counts topic “surfaces” with activity since the handoff marker: topic rows
 * whose updated_at is new, plus threads with message_posted events (topic
 * updated_at can lag behind chat).
 */
function countTopicSurfacesForHandoff(topics = [], events = [], cutoffMs) {
  const threadToTopic = buildThreadToTopicIdMap(topics);
  const seen = new Set();

  for (const topic of topics) {
    const id = String(topic?.id ?? "").trim();
    if (!id) continue;
    if (!isNewerThanCutoff(topic?.updated_at, cutoffMs)) continue;
    seen.add(id);
  }

  for (const event of events) {
    if (String(event?.type ?? "") !== "message_posted") continue;
    if (event?.trashed_at || event?.archived_at) continue;
    if (!isNewerThanCutoff(event?.ts, cutoffMs)) continue;
    const threadId = primaryThreadIdFromEvent(event);
    if (!threadId) continue;
    const topicId = threadToTopic.get(threadId);
    if (topicId) {
      seen.add(topicId);
    } else {
      seen.add(`thread:${threadId}`);
    }
  }

  return seen.size;
}

function compareEventsNewestFirst(left, right) {
  const leftTs = parseTimestampMs(left?.ts);
  const rightTs = parseTimestampMs(right?.ts);

  if (
    Number.isFinite(leftTs) &&
    Number.isFinite(rightTs) &&
    leftTs !== rightTs
  ) {
    return rightTs - leftTs;
  }

  if (Number.isFinite(leftTs) !== Number.isFinite(rightTs)) {
    return Number.isFinite(leftTs) ? -1 : 1;
  }

  return String(right?.id ?? "").localeCompare(String(left?.id ?? ""));
}

function updateLatestTimestamp(currentMax, value) {
  const parsed = parseTimestampMs(value);
  return Number.isFinite(parsed) ? Math.max(currentMax, parsed) : currentMax;
}

export function homeHandoffStorageKey(organizationSlug, workspaceSlug) {
  const org = normalizeStorageSegment(organizationSlug);
  const workspace = normalizeStorageSegment(workspaceSlug);
  return `anx.home.handoff.lastRead.${HOME_HANDOFF_STORAGE_VERSION}.${org}.${workspace}`;
}

export function readHomeHandoffMarker(organizationSlug, workspaceSlug) {
  if (typeof localStorage === "undefined") return "";
  const key = homeHandoffStorageKey(organizationSlug, workspaceSlug);
  const stored = String(localStorage.getItem(key) ?? "").trim();
  return Number.isFinite(parseMarkerMs(stored)) ? stored : "";
}

export function writeHomeHandoffMarker(
  organizationSlug,
  workspaceSlug,
  markerIso,
) {
  if (typeof localStorage === "undefined") return;
  const key = homeHandoffStorageKey(organizationSlug, workspaceSlug);
  const normalized = String(markerIso ?? "").trim();

  if (!normalized) {
    localStorage.removeItem(key);
    return;
  }

  if (!Number.isFinite(parseMarkerMs(normalized))) {
    return;
  }

  localStorage.setItem(key, normalized);
}

export function isHomeTimelineEventIncluded(event) {
  const type = String(event?.type ?? "").trim();
  if (!type) return true;
  if (HOME_EXCLUDED_EVENT_TYPES.has(type)) return false;
  if (HOME_INCLUDED_EVENT_TYPES.has(type)) return true;
  return !KNOWN_EVENT_TYPES.has(type);
}

export function filterHomeTimelineEvents(
  events = [],
  { markerIso = "", limit = 10 } = {},
) {
  const cutoffMs = parseMarkerMs(markerIso);

  return [...events]
    .filter((event) => {
      if (event?.trashed_at || event?.archived_at) return false;
      if (!isHomeTimelineEventIncluded(event)) return false;
      return isNewerThanCutoff(event?.ts, cutoffMs);
    })
    .sort(compareEventsNewestFirst)
    .slice(0, limit);
}

function homeHandoffRefPrefixToPillId(prefix) {
  if (prefix === "inbox") return "inbox";
  if (prefix === "topic" || prefix === "thread") return "topics";
  if (prefix === "board" || prefix === "card") return "boards";
  if (
    prefix === "document" ||
    prefix === "artifact" ||
    prefix === "document_revision"
  ) {
    return "docs-proof";
  }
  return null;
}

/**
 * Maps a workspace event to a Home “pill” id (`inbox` | `topics` | `boards` |
 * `docs-proof`) for filter chips. Ref-first, then `thread_id` / `type` fallback.
 */
export function homeHandoffEventPillId(event) {
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  for (const want of HOME_TIMELINE_REF_PREFIX_PRIORITY) {
    const matched = refs.find((r) => splitTypedRef(r).prefix === want);
    if (!matched) continue;
    const p = splitTypedRef(matched).prefix;
    const pill = homeHandoffRefPrefixToPillId(p);
    if (pill) return pill;
  }

  if (
    String(event?.type ?? "") === "message_posted" &&
    primaryThreadIdFromEvent(event)
  ) {
    return "topics";
  }

  const t = String(event?.type ?? "");
  if (t.startsWith("card_")) return "boards";
  if (t === "message_posted") {
    return "topics";
  }
  if (t === "exception_raised") {
    return "topics";
  }
  return "topics";
}

export function buildHomeChangeCards({
  inboxItems = [],
  topics = [],
  boards = [],
  documents = [],
  artifacts = [],
  events = [],
  markerIso = "",
} = {}) {
  const cutoffMs = parseMarkerMs(markerIso);

  return [
    {
      id: "inbox",
      label: "Inbox changes",
      count: countDistinct(inboxItems, cutoffMs, (item) =>
        readSourceEventTime(item),
      ),
    },
    {
      id: "topics",
      label: "Topic changes",
      count: countTopicSurfacesForHandoff(topics, events, cutoffMs),
    },
    {
      id: "boards",
      label: "Board changes",
      count: countDistinct(boards, cutoffMs, (board) => board?.updated_at),
    },
    {
      id: "docs-proof",
      label: "Docs / Proof changes",
      count:
        countDistinct(documents, cutoffMs, (document) => document?.updated_at) +
        countDistinct(artifacts, cutoffMs, (artifact) => artifact?.created_at),
    },
  ];
}

export function selectHomeInboxPreview(items = [], { limit = 3, now } = {}) {
  return sortInboxItems(items, { now }).slice(0, limit);
}

export function computeNextHomeHandoffMarker({
  markerIso = "",
  inboxItems = [],
  topics = [],
  boards = [],
  documents = [],
  artifacts = [],
  events = [],
  now = Date.now(),
} = {}) {
  const existingMarkerMs = parseMarkerMs(markerIso);
  const cutoffMs = existingMarkerMs;
  let latestTimestampMs = Number.isFinite(existingMarkerMs)
    ? existingMarkerMs
    : Number.NEGATIVE_INFINITY;

  for (const item of inboxItems) {
    latestTimestampMs = updateLatestTimestamp(
      latestTimestampMs,
      readSourceEventTime(item),
    );
  }

  for (const topic of topics) {
    if (!isNewerThanCutoff(topic?.updated_at, cutoffMs)) continue;
    latestTimestampMs = updateLatestTimestamp(
      latestTimestampMs,
      topic?.updated_at,
    );
  }

  for (const board of boards) {
    if (!isNewerThanCutoff(board?.updated_at, cutoffMs)) continue;
    latestTimestampMs = updateLatestTimestamp(
      latestTimestampMs,
      board?.updated_at,
    );
  }

  for (const document of documents) {
    if (!isNewerThanCutoff(document?.updated_at, cutoffMs)) continue;
    latestTimestampMs = updateLatestTimestamp(
      latestTimestampMs,
      document?.updated_at,
    );
  }

  for (const artifact of artifacts) {
    if (!isNewerThanCutoff(artifact?.created_at, cutoffMs)) continue;
    latestTimestampMs = updateLatestTimestamp(
      latestTimestampMs,
      artifact?.created_at,
    );
  }

  for (const event of filterHomeTimelineEvents(events, {
    markerIso,
    limit: 10_000,
  })) {
    latestTimestampMs = updateLatestTimestamp(latestTimestampMs, event?.ts);
  }

  const inboxPreview = selectHomeInboxPreview(inboxItems, { limit: 3, now });
  for (const item of inboxPreview) {
    latestTimestampMs = updateLatestTimestamp(
      latestTimestampMs,
      readSourceEventTime(item),
    );
  }

  if (Number.isFinite(latestTimestampMs)) {
    return timestampToIso(latestTimestampMs);
  }

  return timestampToIso(
    typeof now === "number" ? now : Date.parse(String(now)),
  );
}
