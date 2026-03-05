import { resolveRefLink } from "./refLinkModel.js";

const EVENT_TYPE_LABELS = {
  message_posted: "Message posted",
  work_order_created: "Work order created",
  receipt_added: "Receipt added",
  review_completed: "Review completed",
  decision_needed: "Needs decision",
  decision_made: "Decision made",
  snapshot_updated: "Details updated",
  commitment_created: "Commitment created",
  commitment_status_changed: "Commitment status changed",
  exception_raised: "Exception raised",
  inbox_item_acknowledged: "Item acknowledged",
};

const KNOWN_EVENT_TYPES = new Set(Object.keys(EVENT_TYPE_LABELS));

export function toTimelineViewEvent(event, options = {}) {
  const type = String(event?.type ?? "");
  const isKnownType = KNOWN_EVENT_TYPES.has(type);
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  const threadId = options.threadId ?? event?.thread_id ?? "";

  return {
    ...event,
    refs,
    isKnownType,
    typeLabel: EVENT_TYPE_LABELS[type] ?? "Unknown event type",
    rawType: type,
    changedFields:
      type === "snapshot_updated" &&
      Array.isArray(event?.payload?.changed_fields)
        ? event.payload.changed_fields
        : [],
    resolvedRefs: refs.map((refValue) =>
      resolveRefLink(refValue, { threadId }),
    ),
  };
}

export function toTimelineView(events = [], options = {}) {
  return events.map((event) => toTimelineViewEvent(event, options));
}
