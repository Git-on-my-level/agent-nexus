import { parseTimestampMs } from "./dateUtils.js";

export const INBOX_CATEGORY_ORDER = [
  "decision_needed",
  "intervention_needed",
  "exception",
  "commitment_risk",
];

export const INBOX_CATEGORY_LABELS = {
  decision_needed: "Needs Decision",
  intervention_needed: "Needs Intervention",
  exception: "Exception",
  commitment_risk: "At Risk",
};

export const INBOX_CATEGORY_DESCRIPTIONS = {
  decision_needed: "Decision event pending",
  intervention_needed: "Human action required",
  exception: "Error or anomaly flagged",
  commitment_risk: "Commitment deadline approaching",
};

export function getInboxCategoryLabel(category) {
  return INBOX_CATEGORY_LABELS[category] ?? category;
}

export const INBOX_URGENCY_LEVELS = ["immediate", "high", "normal"];

export const INBOX_URGENCY_LABELS = {
  immediate: "Immediate",
  high: "High",
  normal: "Normal",
};

const INBOX_CATEGORY_URGENCY_BASE = {
  exception: 90,
  decision_needed: 76,
  intervention_needed: 74,
  commitment_risk: 62,
};

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
  const category = String(item?.category ?? "unknown");

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
  return {
    ...item,
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
    const category = String(item?.category ?? "unknown");

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
    const category = String(item?.category ?? "unknown");
    counts[category] = (counts[category] ?? 0) + 1;
  }
  return counts;
}
