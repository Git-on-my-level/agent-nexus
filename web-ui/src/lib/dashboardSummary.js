import { INBOX_CATEGORY_ORDER, getInboxCategoryLabel } from "./inboxUtils";
import { computeStaleness } from "./threadFilters";

function parseTimestamp(value) {
  if (!value) {
    return Number.NaN;
  }

  return Date.parse(String(value));
}

function compareByTimestampDesc(leftValue, rightValue) {
  const leftTs = parseTimestamp(leftValue);
  const rightTs = parseTimestamp(rightValue);
  const leftHasTs = Number.isFinite(leftTs);
  const rightHasTs = Number.isFinite(rightTs);

  if (leftHasTs && rightHasTs && leftTs !== rightTs) {
    return rightTs - leftTs;
  }

  if (leftHasTs !== rightHasTs) {
    return leftHasTs ? -1 : 1;
  }

  return 0;
}

export function buildInboxCategorySummary(items = []) {
  const counts = new Map();

  for (const item of items) {
    const category = String(item?.category ?? "unknown");
    counts.set(category, (counts.get(category) ?? 0) + 1);
  }

  const orderedCategories = [
    ...INBOX_CATEGORY_ORDER,
    ...[...counts.keys()].filter(
      (category) => !INBOX_CATEGORY_ORDER.includes(category),
    ),
  ];

  return orderedCategories.map((category) => ({
    category,
    label: getInboxCategoryLabel(category),
    count: counts.get(category) ?? 0,
  }));
}

export function buildThreadHealthSummary(threads = []) {
  let openCount = 0;
  let staleCount = 0;
  let highPriorityCount = 0;

  for (const thread of threads) {
    const status = String(thread?.status ?? "");
    const isOpen = status !== "closed";

    if (isOpen) {
      openCount += 1;

      if (computeStaleness(thread).stale) {
        staleCount += 1;
      }

      const priority = String(thread?.priority ?? "");
      if (priority === "p0" || priority === "p1") {
        highPriorityCount += 1;
      }
    }
  }

  return {
    totalCount: threads.length,
    openCount,
    staleCount,
    highPriorityCount,
  };
}

export function selectRecentlyUpdatedThreads(threads = [], limit = 5) {
  return [...threads]
    .sort((left, right) => {
      const byTimestamp = compareByTimestampDesc(
        left?.updated_at,
        right?.updated_at,
      );
      if (byTimestamp !== 0) {
        return byTimestamp;
      }

      return String(left?.id ?? "").localeCompare(String(right?.id ?? ""));
    })
    .slice(0, limit);
}

export function buildArtifactKindSummary(artifacts = []) {
  const counts = {
    review: 0,
    receipt: 0,
    work_order: 0,
    other: 0,
  };

  for (const artifact of artifacts) {
    const kind = String(artifact?.kind ?? "");

    if (kind === "review" || kind === "receipt" || kind === "work_order") {
      counts[kind] += 1;
      continue;
    }

    counts.other += 1;
  }

  return counts;
}

export function selectRecentArtifacts(artifacts = [], limit = 5) {
  return [...artifacts]
    .sort((left, right) => {
      const byTimestamp = compareByTimestampDesc(
        left?.created_at,
        right?.created_at,
      );
      if (byTimestamp !== 0) {
        return byTimestamp;
      }

      return String(left?.id ?? "").localeCompare(String(right?.id ?? ""));
    })
    .slice(0, limit);
}
