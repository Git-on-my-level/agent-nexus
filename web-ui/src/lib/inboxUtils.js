import { parseTimestampMs } from "./dateUtils.js";

export const INBOX_CATEGORY_ORDER = [
  "decision_needed",
  "intervention_needed",
  "work_item_risk",
  "stale_topic",
  "document_attention",
];

export const INBOX_CATEGORY_LABELS = {
  decision_needed: "Needs Decision",
  intervention_needed: "Needs Intervention",
  work_item_risk: "Work item risk",
  stale_topic: "Stale Topic",
  document_attention: "Document Attention",
};

export const INBOX_CATEGORY_DESCRIPTIONS = {
  decision_needed: "Decision event pending",
  intervention_needed: "Human action required",
  work_item_risk: "Work item risk needs review",
  stale_topic: "Topic appears stale",
  document_attention: "Document needs attention",
};

export function getInboxCategoryLabel(category) {
  return INBOX_CATEGORY_LABELS[normalizeInboxCategory(category)] ?? category;
}

export const INBOX_URGENCY_LEVELS = ["immediate", "high", "normal"];

export const INBOX_URGENCY_LABELS = {
  immediate: "Immediate",
  high: "High",
  normal: "Normal",
};

const INBOX_CATEGORY_URGENCY_BASE = {
  decision_needed: 76,
  intervention_needed: 74,
  work_item_risk: 66,
  stale_topic: 90,
  document_attention: 58,
};

const INBOX_CATEGORY_ALIASES = {
  risk_review: "work_item_risk",
  exception: "stale_topic",
};

const INBOX_SUBJECT_LABELS = {
  topic: "Topic",
  card: "Card",
  board: "Board",
  document: "Document",
  thread: "Topic",
};

export function normalizeInboxCategory(category) {
  const normalized = String(category ?? "").trim();
  return INBOX_CATEGORY_ALIASES[normalized] ?? normalized;
}

export function splitTypedRef(refValue) {
  const raw = String(refValue ?? "").trim();
  const separatorIndex = raw.indexOf(":");
  if (separatorIndex <= 0 || separatorIndex >= raw.length - 1) {
    return { prefix: "", id: "" };
  }
  return {
    prefix: raw.slice(0, separatorIndex).trim(),
    id: raw.slice(separatorIndex + 1).trim(),
  };
}

export function normalizeTypedRef(refValue) {
  const { prefix, id } = splitTypedRef(refValue);
  if (!prefix || !id) {
    return "";
  }
  return `${prefix}:${id}`;
}

export function getInboxSubjectRef(item) {
  const explicit = normalizeTypedRef(item?.subject_ref);
  if (explicit) return explicit;

  const topicId = String(item?.topic_id ?? "").trim();
  if (topicId) return `topic:${topicId}`;

  const cardId = String(item?.card_id ?? item?.work_item_id ?? "").trim();
  if (cardId) return `card:${cardId}`;

  const boardId = String(item?.board_id ?? "").trim();
  if (boardId) return `board:${boardId}`;

  const documentId = String(item?.document_id ?? "").trim();
  if (documentId) return `document:${documentId}`;

  const threadId = String(item?.thread_id ?? "").trim();
  if (threadId) return `thread:${threadId}`;

  return "";
}

export function getInboxSubjectKind(item) {
  return splitTypedRef(getInboxSubjectRef(item)).prefix;
}

export function getInboxSubjectId(item) {
  return splitTypedRef(getInboxSubjectRef(item)).id;
}

export function getInboxSubjectLabel(item) {
  const subjectRef = getInboxSubjectRef(item);
  if (!subjectRef) {
    return "";
  }

  const { prefix, id } = splitTypedRef(subjectRef);
  const label = INBOX_SUBJECT_LABELS[prefix] ?? prefix;
  const title = String(item?.subject_title ?? item?.subject_name ?? "").trim();
  return title ? `${label}: ${title}` : `${label}: ${id}`;
}

export function getInboxUrgencyLabel(level) {
  const normalizedLevel = String(level ?? "").trim();
  return (
    INBOX_URGENCY_LABELS[normalizedLevel] ?? (normalizedLevel || "Unknown")
  );
}

export function readSourceEventTime(item) {
  return (
    item?.source_event_time ??
    item?.source_event_ts ??
    item?.source_event?.ts ??
    null
  );
}

function getItemTitle(item) {
  return String(item?.title ?? item?.summary ?? "");
}

function readNowTimestamp(options = {}) {
  const now = options.now ?? Date.now();
  if (now instanceof Date) {
    const nowTs = now.getTime();
    return Number.isFinite(nowTs) ? nowTs : Date.now();
  }

  const numericNow = Number(now);
  if (Number.isFinite(numericNow)) {
    return numericNow;
  }

  const parsedNow = parseTimestampMs(now);
  return Number.isFinite(parsedNow) ? parsedNow : Date.now();
}

function formatAgeLabel(ageHours) {
  if (!Number.isFinite(ageHours)) {
    return "";
  }

  if (ageHours < 1) {
    return "<1h old";
  }

  if (ageHours < 24) {
    return `${Math.floor(ageHours)}h old`;
  }

  return `${Math.floor(ageHours / 24)}d old`;
}

export function deriveInboxUrgency(item, options = {}) {
  const nowTs = readNowTimestamp(options);
  const sourceEventTime = readSourceEventTime(item);
  const sourceEventTs = parseTimestampMs(sourceEventTime);
  const hasSourceEventTime = Number.isFinite(sourceEventTs);
  const ageHours = hasSourceEventTime
    ? Math.max(0, (nowTs - sourceEventTs) / (60 * 60 * 1000))
    : Number.NaN;
  const category = normalizeInboxCategory(item?.category ?? "unknown");

  let score = INBOX_CATEGORY_URGENCY_BASE[category] ?? 54;

  if (hasSourceEventTime) {
    if (ageHours >= 72) score += 14;
    else if (ageHours >= 24) score += 10;
    else if (ageHours >= 8) score += 6;
    else if (ageHours >= 2) score += 3;
  }

  score = Math.min(100, Math.max(0, score));

  let level = "normal";
  if (score >= 90) level = "immediate";
  else if (score >= 74) level = "high";

  return {
    level,
    label: getInboxUrgencyLabel(level),
    score,
    ageHours,
    ageLabel: formatAgeLabel(ageHours),
    hasSourceEventTime,
    sourceEventTime,
    inferredFrom: "category + source event age",
  };
}

export function enrichInboxItem(item, options = {}) {
  const urgency = deriveInboxUrgency(item, options);
  const subjectRef = getInboxSubjectRef(item);
  const subject = splitTypedRef(subjectRef);
  return {
    ...item,
    category: normalizeInboxCategory(item?.category ?? "unknown"),
    subject_ref: subjectRef || item?.subject_ref || "",
    subject_kind: subject.prefix,
    subject_id: subject.id,
    urgency_level: urgency.level,
    urgency_label: urgency.label,
    urgency_score: urgency.score,
    age_hours: urgency.ageHours,
    age_label: urgency.ageLabel,
    has_source_event_time: urgency.hasSourceEventTime,
    source_event_time: urgency.sourceEventTime,
    urgency_inferred_from: urgency.inferredFrom,
  };
}

export function summarizeInboxUrgency(items = [], options = {}) {
  return items.reduce(
    (counts, item) => {
      const { level } = deriveInboxUrgency(item, options);
      if (level === "immediate") counts.immediate += 1;
      else if (level === "high") counts.high += 1;
      else counts.normal += 1;
      return counts;
    },
    { immediate: 0, high: 0, normal: 0 },
  );
}

export function sortInboxItems(items, options = {}) {
  const nowTs = readNowTimestamp(options);
  const decoratedItems = [...items].map((item) => ({
    item,
    urgency: deriveInboxUrgency(item, { now: nowTs }),
    sourceEventTs: parseTimestampMs(readSourceEventTime(item)),
    title: getItemTitle(item),
    id: String(item?.id ?? ""),
  }));

  return decoratedItems
    .sort((left, right) => {
      if (left.urgency.score !== right.urgency.score) {
        return right.urgency.score - left.urgency.score;
      }

      const leftHasTs = Number.isFinite(left.sourceEventTs);
      const rightHasTs = Number.isFinite(right.sourceEventTs);

      if (
        leftHasTs &&
        rightHasTs &&
        left.sourceEventTs !== right.sourceEventTs
      ) {
        return left.sourceEventTs - right.sourceEventTs;
      }

      if (leftHasTs !== rightHasTs) {
        return leftHasTs ? -1 : 1;
      }

      const titleCompare = left.title.localeCompare(right.title);
      if (titleCompare !== 0) {
        return titleCompare;
      }

      return left.id.localeCompare(right.id);
    })
    .map(({ item }) => item);
}

export function groupInboxItems(items = [], options = {}) {
  const grouped = new Map();

  INBOX_CATEGORY_ORDER.forEach((category) => grouped.set(category, []));

  for (const item of items) {
    const category = normalizeInboxCategory(item?.category ?? "unknown");

    if (!grouped.has(category)) {
      grouped.set(category, []);
    }

    grouped.get(category).push(item);
  }

  const knownGroups = INBOX_CATEGORY_ORDER.map((category) => ({
    category,
    items: sortInboxItems(grouped.get(category) ?? [], options),
  }));

  const extraGroups = [...grouped.entries()]
    .filter(([category]) => !INBOX_CATEGORY_ORDER.includes(category))
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([category, categoryItems]) => ({
      category,
      items: sortInboxItems(categoryItems, options),
    }));

  return [...knownGroups, ...extraGroups];
}

export function summarizeInboxByCategory(items = []) {
  const counts = {};
  for (const category of INBOX_CATEGORY_ORDER) {
    counts[category] = 0;
  }
  for (const item of items) {
    const category = normalizeInboxCategory(item?.category ?? "unknown");
    counts[category] = (counts[category] ?? 0) + 1;
  }
  return counts;
}
