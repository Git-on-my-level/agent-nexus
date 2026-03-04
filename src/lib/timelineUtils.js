import { resolveRefLink } from "./refLinkModel.js";

const KNOWN_EVENT_TYPES = new Set([
  "message_posted",
  "work_order_created",
  "receipt_added",
  "review_completed",
  "decision_needed",
  "decision_made",
  "snapshot_updated",
  "commitment_created",
  "commitment_status_changed",
  "exception_raised",
  "inbox_item_acknowledged",
]);

export function toTimelineViewEvent(event, options = {}) {
  const type = String(event?.type ?? "");
  const isKnownType = KNOWN_EVENT_TYPES.has(type);
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  const threadId = options.threadId ?? event?.thread_id ?? "";

  return {
    ...event,
    refs,
    isKnownType,
    typeLabel: isKnownType ? type : "Unknown event type",
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
