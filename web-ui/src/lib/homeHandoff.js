import { parseTimestampMs } from "./dateUtils.js";
import {
  readSourceEventTime,
  sortInboxItems,
  splitTypedRef,
} from "./inboxUtils.js";
import { KNOWN_EVENT_TYPES } from "./timelineUtils.js";

const HOME_HANDOFF_STORAGE_VERSION = "v1";

const HOME_INCLUDED_EVENT_TYPES = new Set([
  "decision_needed",
  "intervention_needed",
  "decision_made",
  "exception_raised",
  "thread_created",
  "thread_updated",
  "card_created",
  "card_updated",
  "card_moved",
  "card_resolved",
  "receipt_added",
  "review_completed",
  "message_posted",
]);

const HOME_EXCLUDED_EVENT_TYPES = new Set(["inbox_item_acknowledged"]);

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

/** Same priority as Home `homeEventPrimaryRef` for a stable “primary” ref. */
const HOME_EVENT_REF_PRIORITY = [
  "topic",
  "thread",
  "board",
  "document",
  "artifact",
  "card",
  "inbox",
  "url",
  "event",
];

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
  for (const want of HOME_EVENT_REF_PRIORITY) {
    const matched = refs.find((r) => splitTypedRef(r).prefix === want);
    if (!matched) continue;
    const p = splitTypedRef(matched).prefix;
    const pill = homeHandoffRefPrefixToPillId(p);
    if (pill) return pill;
  }

  if (String(event?.type ?? "") === "message_posted" && primaryThreadIdFromEvent(event)) {
    return "topics";
  }

  const t = String(event?.type ?? "");
  if (t.startsWith("card_")) return "boards";
  if (t === "message_posted" || t === "thread_created" || t === "thread_updated") {
    return "topics";
  }
  if (t === "receipt_added" || t === "review_completed") {
    return "docs-proof";
  }
  if (
    t === "decision_needed" ||
    t === "intervention_needed" ||
    t === "exception_raised" ||
    t === "decision_made"
  ) {
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
