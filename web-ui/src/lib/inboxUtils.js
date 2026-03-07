export const INBOX_CATEGORY_ORDER = [
  "decision_needed",
  "exception",
  "commitment_risk",
];

export const INBOX_CATEGORY_LABELS = {
  decision_needed: "Needs Decision",
  exception: "Exception",
  commitment_risk: "At Risk",
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
  commitment_risk: 62,
};

export function getInboxUrgencyLabel(level) {
  return INBOX_URGENCY_LABELS[level] ?? "Normal";
}

export function readSourceEventTime(item) {
  return (
    item?.source_event_time ??
    item?.source_event_ts ??
    item?.source_event?.ts ??
    null
  );
}

function parseTimestamp(value) {
  if (!value) {
    return Number.NaN;
  }

  return Date.parse(String(value));
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

  const parsedNow = parseTimestamp(now);
  return Number.isFinite(parsedNow) ? parsedNow : Date.now();
}

function formatAgeLabel(ageHours) {
  if (!Number.isFinite(ageHours)) {
    return "No source time";
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
  const sourceEventTs = parseTimestamp(sourceEventTime);
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

  return [...items].sort((left, right) => {
    const leftUrgency = deriveInboxUrgency(left, { now: nowTs });
    const rightUrgency = deriveInboxUrgency(right, { now: nowTs });

    if (leftUrgency.score !== rightUrgency.score) {
      return rightUrgency.score - leftUrgency.score;
    }

    const leftTs = parseTimestamp(readSourceEventTime(left));
    const rightTs = parseTimestamp(readSourceEventTime(right));
    const leftHasTs = Number.isFinite(leftTs);
    const rightHasTs = Number.isFinite(rightTs);

    if (leftHasTs && rightHasTs && leftTs !== rightTs) {
      return leftTs - rightTs;
    }

    if (leftHasTs !== rightHasTs) {
      return leftHasTs ? -1 : 1;
    }

    const titleCompare = getItemTitle(left).localeCompare(getItemTitle(right));
    if (titleCompare !== 0) {
      return titleCompare;
    }

    return String(left?.id ?? "").localeCompare(String(right?.id ?? ""));
  });
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
