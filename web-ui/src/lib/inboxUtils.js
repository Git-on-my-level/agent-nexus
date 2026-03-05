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

function readSourceEventTime(item) {
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

export function sortInboxItems(items) {
  return [...items].sort((left, right) => {
    const leftTs = parseTimestamp(readSourceEventTime(left));
    const rightTs = parseTimestamp(readSourceEventTime(right));
    const leftHasTs = Number.isFinite(leftTs);
    const rightHasTs = Number.isFinite(rightTs);

    if (leftHasTs && rightHasTs && leftTs !== rightTs) {
      return rightTs - leftTs;
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

export function groupInboxItems(items = []) {
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
    items: sortInboxItems(grouped.get(category) ?? []),
  }));

  const extraGroups = [...grouped.entries()]
    .filter(([category]) => !INBOX_CATEGORY_ORDER.includes(category))
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([category, categoryItems]) => ({
      category,
      items: sortInboxItems(categoryItems),
    }));

  return [...knownGroups, ...extraGroups];
}
