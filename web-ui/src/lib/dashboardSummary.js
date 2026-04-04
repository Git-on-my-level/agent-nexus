import { parseTimestampMs } from "./dateUtils.js";
import {
  INBOX_CATEGORY_ORDER,
  getInboxCategoryLabel,
  normalizeInboxCategory,
} from "./inboxUtils";
import { computeStaleness } from "./threadFilters";

function compareByTimestampDesc(leftValue, rightValue) {
  const leftTs = parseTimestampMs(leftValue);
  const rightTs = parseTimestampMs(rightValue);
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
    const category = normalizeInboxCategory(item?.category ?? "unknown");
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

function summaryGroupKey(artifact) {
  return `${artifact?.thread_id ?? ""}||${artifact?.kind ?? ""}||${String(
    artifact?.summary ?? "",
  )
    .trim()
    .toLowerCase()}`;
}

function countRefPredecessorDepth(artifact, byId) {
  let depth = 0;
  const visited = new Set([artifact?.id]);
  let current = artifact;

  while (current) {
    const predRef = (current.refs ?? []).find((ref) => {
      if (!ref.startsWith("artifact:")) return false;
      const predId = ref.slice("artifact:".length);
      const pred = byId.get(predId);
      return pred && pred.kind === current.kind && !visited.has(predId);
    });
    if (!predRef) break;
    const predId = predRef.slice("artifact:".length);
    visited.add(predId);
    depth += 1;
    current = byId.get(predId);
  }

  return depth;
}

export function threadHealthSentence(summary) {
  const { openCount, staleCount, highPriorityCount } = summary;

  if (openCount === 0) {
    return "No open threads.";
  }

  if (staleCount === 0 && highPriorityCount === 0) {
    return openCount === 1
      ? "1 open thread is on track."
      : `All ${openCount} open threads are on track.`;
  }

  if (staleCount > 0 && highPriorityCount === 0) {
    return staleCount === 1
      ? "1 thread may need a check-in."
      : `${staleCount} threads may need a check-in.`;
  }

  if (highPriorityCount > 0 && staleCount === 0) {
    return highPriorityCount === 1
      ? "1 high-priority thread needs attention."
      : `${highPriorityCount} high-priority threads need attention.`;
  }

  const stalePart =
    staleCount === 1 ? "1 stale thread" : `${staleCount} stale threads`;
  const highPart =
    highPriorityCount === 1
      ? "1 high-priority thread"
      : `${highPriorityCount} high-priority threads`;
  return `${stalePart} and ${highPart} need attention.`;
}

export function inboxSummarySentence(categorySummary) {
  const total = categorySummary.reduce((sum, entry) => sum + entry.count, 0);

  if (total === 0) {
    return "Inbox is clear.";
  }

  const decisionEntry = categorySummary.find(
    (entry) => entry.category === "decision_needed",
  );
  const decisions = decisionEntry ? decisionEntry.count : 0;

  const itemWord = total === 1 ? "work item needs" : "work items need";
  const base = `${total} ${itemWord} your attention`;

  if (decisions > 0) {
    const decisionWord = decisions === 1 ? "decision" : "decisions";
    return `${base}, including ${decisions} ${decisionWord}.`;
  }

  return `${base}.`;
}

export function selectRecentArtifacts(artifacts = [], limit = 5) {
  const live = (artifacts ?? []).filter((a) => !a?.tombstoned_at);
  const byId = new Map(live.map((a) => [a.id, a]));

  // Artifacts superseded by a newer artifact of the same kind via an explicit artifact: ref.
  const supersededByRef = new Set();
  for (const artifact of live) {
    for (const ref of artifact.refs ?? []) {
      if (!ref.startsWith("artifact:")) continue;
      const predId = ref.slice("artifact:".length);
      const pred = byId.get(predId);
      if (pred && pred.kind === artifact.kind) {
        supersededByRef.add(predId);
      }
    }
  }

  // Group the remaining artifacts by (thread, kind, summary). Within each group
  // keep only the newest; older copies are superseded by summary heuristic.
  const summaryGroups = new Map();
  for (const artifact of live) {
    if (supersededByRef.has(artifact.id)) continue;
    const key = summaryGroupKey(artifact);
    if (!summaryGroups.has(key)) summaryGroups.set(key, []);
    summaryGroups.get(key).push(artifact);
  }

  const supersededBySummary = new Set();
  for (const group of summaryGroups.values()) {
    if (group.length <= 1) continue;
    group.sort((a, b) => compareByTimestampDesc(a?.created_at, b?.created_at));
    for (let i = 1; i < group.length; i++) {
      supersededBySummary.add(group[i].id);
    }
  }

  // Leaf artifacts are those not excluded by either rule.
  const leafArtifacts = live.filter(
    (a) => !supersededByRef.has(a.id) && !supersededBySummary.has(a.id),
  );

  // Annotate each leaf with whether it is an update and how many versions exist.
  const annotated = leafArtifacts.map((artifact) => {
    const refDepth = countRefPredecessorDepth(artifact, byId);
    const hasRefPredecessor = refDepth > 0;

    const summaryGroup = summaryGroups.get(summaryGroupKey(artifact)) ?? [];
    const hasSummaryPredecessor = summaryGroup.length > 1;

    const isUpdate = hasRefPredecessor || hasSummaryPredecessor;
    const versionCount = hasRefPredecessor
      ? refDepth + 1
      : hasSummaryPredecessor
        ? summaryGroup.length
        : 1;

    return { ...artifact, isUpdate, versionCount };
  });

  return annotated
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
