import { resolveRefLink } from "./refLinkModel.js";

const SNAPSHOT_FIELD_LABELS = {
  current_summary: "Summary",
  next_actions: "Next actions",
  cadence: "Schedule",
  open_cards: "Open cards",
  key_artifacts: "Key artifacts",
  title: "Title",
  status: "Status",
  priority: "Priority",
  tags: "Tags",
  next_check_in_at: "Next check-in",
  type: "Type",
};

const EVENT_TYPE_LABELS = {
  message_posted: "Message posted",
  receipt_added: "Receipt added",
  review_completed: "Review completed",
  decision_needed: "Needs decision",
  intervention_needed: "Needs intervention",
  decision_made: "Decision made",
  thread_updated: "Details updated",
  thread_created: "Thread created",
  card_created: "Card created",
  card_updated: "Card updated",
  card_moved: "Card moved",
  card_resolved: "Card resolved",
  exception_raised: "Exception raised",
  inbox_item_acknowledged: "Item acknowledged",
};

export const KNOWN_EVENT_TYPES = new Set(Object.keys(EVENT_TYPE_LABELS));

const EVENT_TYPE_DOT_CLASSES = {
  message_posted: "bg-indigo-400",
  receipt_added: "bg-emerald-400",
  review_completed: "bg-amber-400",
  decision_needed: "bg-red-400",
  intervention_needed: "bg-cyan-400",
  decision_made: "bg-emerald-400",
  thread_updated: "bg-gray-400",
  thread_created: "bg-gray-400",
  card_created: "bg-purple-400",
  card_updated: "bg-purple-400",
  card_moved: "bg-purple-400",
  card_resolved: "bg-purple-400",
  exception_raised: "bg-red-400",
  inbox_item_acknowledged: "bg-teal-400",
};

export function eventTypeDotClass(type) {
  return EVENT_TYPE_DOT_CLASSES[type] ?? "bg-gray-500";
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value
    : {};
}

function artifactLabel(artifact, id) {
  const record = asObject(artifact);
  const summary = String(record.summary ?? record.title ?? "").trim();
  if (summary) {
    return summary;
  }
  const kind = String(record.kind ?? "Artifact").trim();
  return `${kind} ${id}`.trim();
}

function documentLabel(document, id) {
  const record = asObject(document);
  const title = String(record.title ?? "").trim();
  if (title) {
    return title;
  }
  return `Document ${id}`.trim();
}

function documentRevisionLabel(revision, id, documents = {}) {
  const record = asObject(revision);
  const documentId = String(record.document_id ?? "").trim();
  const document = documentId ? asObject(documents[documentId]) : {};
  const title = String(document.title ?? "").trim();
  const revisionNumber = record.revision_number;

  if (title && Number.isFinite(Number(revisionNumber))) {
    return `${title} revision ${revisionNumber}`.trim();
  }

  if (title) {
    return `${title} revision`.trim();
  }

  return `Document revision ${id}`.trim();
}

export function buildTimelineRefLabelHints(
  artifacts = {},
  documents = {},
  documentRevisions = {},
) {
  const hints = {};

  for (const [artifactId, artifact] of Object.entries(asObject(artifacts))) {
    const id = String(artifactId ?? "").trim();
    if (!id) continue;
    hints[`artifact:${id}`] = artifactLabel(artifact, id);
  }

  for (const [documentId, document] of Object.entries(asObject(documents))) {
    const id = String(documentId ?? "").trim();
    if (!id) continue;
    hints[`document:${id}`] = documentLabel(document, id);
  }

  for (const [revisionId, revision] of Object.entries(
    asObject(documentRevisions),
  )) {
    const id = String(revisionId ?? "").trim();
    if (!id) continue;
    hints[`document_revision:${id}`] = documentRevisionLabel(
      revision,
      id,
      documents,
    );
  }

  return hints;
}

export function toTimelineViewEvent(event, options = {}) {
  const type = String(event?.type ?? "");
  const isKnownType = KNOWN_EVENT_TYPES.has(type);
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  const threadId = options.threadId ?? event?.thread_id ?? "";
  const labelHints =
    options.labelHints ??
    buildTimelineRefLabelHints(
      options.artifacts,
      options.documents,
      options.documentRevisions,
    );

  return {
    ...event,
    refs,
    isKnownType,
    typeLabel: EVENT_TYPE_LABELS[type] ?? "Unknown event type",
    rawType: type,
    changedFields:
      type === "thread_updated" && Array.isArray(event?.payload?.changed_fields)
        ? event.payload.changed_fields.map((f) => SNAPSHOT_FIELD_LABELS[f] ?? f)
        : [],
    resolvedRefs: refs.map((refValue) =>
      resolveRefLink(refValue, {
        threadId,
        humanize: true,
        labelHints,
      }),
    ),
  };
}

function parseEventTimeMs(event) {
  const ts = event?.ts;
  if (ts == null || ts === "") {
    return Number.NEGATIVE_INFINITY;
  }
  const ms = Date.parse(String(ts));
  return Number.isFinite(ms) ? ms : Number.NEGATIVE_INFINITY;
}

function compareEventsNewestFirst(a, b) {
  const tb = parseEventTimeMs(b);
  const ta = parseEventTimeMs(a);
  if (tb !== ta) {
    return tb - ta;
  }
  return String(b.id ?? "").localeCompare(String(a.id ?? ""));
}

export function toTimelineView(events = [], options = {}) {
  const ordered = Array.isArray(events)
    ? [...events].sort(compareEventsNewestFirst)
    : [];
  return ordered.map((event) => toTimelineViewEvent(event, options));
}
