/** Shown under Topic groups (messages, topic lifecycle, human attention). */
export const TOPIC_HOME_EVENT_TYPES = new Set([
  "message_posted",
  "topic_priority_changed",
  "topic_lifecycle_changed",
  "topic_updated",
  "topic_archived",
  "topic_restored",
  "topic_trashed",
  "human_attention_requested",
  "human_attention_responded",
]);

// Backend Home grouping can assign board backing-thread messages to board groups.
// Keep message_posted visible here so unread counts match the rows operators see.
export const BOARD_HOME_EVENT_TYPES = new Set([
  "message_posted",
  "card_created",
  "card_moved",
  "card_closed",
  "card_resolved",
  "card_restored",
  "card_archived",
  "card_trashed",
]);

export const DOCUMENT_HOME_EVENT_TYPES = new Set([
  "document_created",
  "document_revision_created",
  "document_revised",
  "document_trashed",
  "document_restored",
]);

export function filterEventsForHomeSection(events, typeSet) {
  return (events ?? []).filter((e) =>
    typeSet.has(String(e?.type ?? "").trim()),
  );
}
