import { messageEventHrefFromEvent } from "$lib/deepLinkTargets";

export const HOME_FEED_PRESET = "home_feed";

export const HOME_FEED_EVENT_TYPES = new Set([
  "message_posted",
  "card_created",
  "card_moved",
  "card_closed",
  "card_resolved",
  "card_restored",
  "card_archived",
  "card_trashed",
  "topic_priority_changed",
  "topic_lifecycle_changed",
  "topic_updated",
  "topic_archived",
  "topic_restored",
  "topic_trashed",
  "human_attention_requested",
  "human_attention_responded",
  "document_created",
  "document_revision_created",
  "document_revised",
]);

function asText(value) {
  return String(value ?? "").trim();
}

function payloadOf(event) {
  return event && typeof event.payload === "object" && event.payload
    ? event.payload
    : {};
}

function firstText(...values) {
  for (const value of values) {
    const text = asText(value);
    if (text) return text;
  }
  return "";
}

function refId(event, prefix) {
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  const match = refs.find((ref) => asText(ref).startsWith(`${prefix}:`));
  return match ? asText(match).slice(prefix.length + 1) : "";
}

function truncateLines(text, maxLines = 2) {
  const lines = asText(text)
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
  return lines.slice(0, maxLines).join("\n");
}

export function isHomeFeedEvent(event) {
  return HOME_FEED_EVENT_TYPES.has(asText(event?.type));
}

export function normalizeEventRow(
  event,
  { workspaceHref = (path) => path } = {},
) {
  const type = asText(event?.type);
  const payload = payloadOf(event);
  const topicId = refId(event, "topic");
  const cardId = firstText(
    payload.card_id,
    payload.cardId,
    refId(event, "card"),
  );
  const documentId = firstText(
    payload.document_id,
    payload.documentId,
    refId(event, "document"),
  );
  const inboxId = firstText(
    payload.inbox_item_id,
    payload.ask_id,
    refId(event, "inbox"),
  );
  const topicHref = topicId
    ? workspaceHref(`/topics/${encodeURIComponent(topicId)}`)
    : "";

  let label = type || "Event";
  let detail = asText(event?.summary);
  let href =
    topicHref ||
    workspaceHref(`/events#${encodeURIComponent(asText(event?.id))}`);
  let sourceLabel = "";

  if (type === "message_posted") {
    label = "Message";
    detail = truncateLines(
      firstText(payload.body, payload.text, event?.summary),
    );
    href =
      messageEventHrefFromEvent(event, { workspaceHref }) || topicHref || href;
  } else if (type === "card_moved") {
    label = "Card moved";
    detail = firstText(
      [
        firstText(payload.from, payload.from_column_key),
        firstText(payload.to, payload.column_key, payload.to_column_key),
      ]
        .filter(Boolean)
        .join(" -> "),
      event?.summary,
    );
    sourceLabel = firstText(payload.title, payload.card_title, cardId);
    href = cardId
      ? workspaceHref(`/boards?card=${encodeURIComponent(cardId)}`)
      : href;
  } else if (type === "card_created") {
    label = "Card created";
    detail = firstText(payload.title, payload.card_title, event?.summary);
    sourceLabel = firstText(payload.title, payload.card_title, cardId);
    href = cardId
      ? workspaceHref(`/boards?card=${encodeURIComponent(cardId)}`)
      : href;
  } else if (
    ["card_closed", "card_resolved", "card_archived", "card_trashed"].includes(
      type,
    )
  ) {
    label = "Card closed";
    detail = firstText(payload.title, payload.card_title, event?.summary);
    sourceLabel = firstText(payload.title, payload.card_title, cardId);
    href = cardId
      ? workspaceHref(`/boards?card=${encodeURIComponent(cardId)}`)
      : href;
  } else if (type === "card_restored") {
    label = "Card restored";
    detail = firstText(payload.title, payload.card_title, event?.summary);
    sourceLabel = firstText(payload.title, payload.card_title, cardId);
    href = cardId
      ? workspaceHref(`/boards?card=${encodeURIComponent(cardId)}`)
      : href;
  } else if (type === "topic_priority_changed") {
    label = "Priority";
    detail = firstText(
      [payload.from, payload.to].filter(Boolean).join(" -> "),
      event?.summary,
    );
    href = topicHref || href;
  } else if (
    [
      "topic_lifecycle_changed",
      "topic_updated",
      "topic_archived",
      "topic_restored",
      "topic_trashed",
    ].includes(type)
  ) {
    label = "Lifecycle";
    detail = firstText(
      [payload.from, payload.to].filter(Boolean).join(" -> "),
      event?.summary,
    );
    href = topicHref || href;
  } else if (type === "human_attention_requested") {
    label = "Ask opened";
    detail = firstText(payload.title, payload.subject, event?.summary);
    href = inboxId
      ? workspaceHref(`/inbox/${encodeURIComponent(inboxId)}`)
      : href;
  } else if (type === "human_attention_responded") {
    label = "Ask resolved";
    detail = firstText(payload.title, payload.subject, event?.summary);
    href = inboxId
      ? workspaceHref(`/inbox/${encodeURIComponent(inboxId)}`)
      : href;
  } else if (
    [
      "document_created",
      "document_revision_created",
      "document_revised",
    ].includes(type)
  ) {
    label = "Doc";
    detail = firstText(payload.title, payload.document_title, event?.summary);
    sourceLabel = firstText(payload.title, payload.document_title, documentId);
    href = documentId
      ? workspaceHref(`/docs/${encodeURIComponent(documentId)}`)
      : href;
  }

  return {
    id: asText(event?.id),
    actorId: asText(event?.actor_id),
    ts: asText(event?.ts),
    rawType: type,
    label,
    detail,
    href,
    sourceLabel,
    refs: Array.isArray(event?.refs) ? event.refs : [],
    event,
    homeEligible: isHomeFeedEvent(event),
  };
}
