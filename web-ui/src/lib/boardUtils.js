export const BOARD_STATUS_LABELS = {
  active: "Active",
  paused: "Paused",
  closed: "Closed",
};

export const CANONICAL_BOARD_COLUMNS = [
  { key: "backlog", title: "Backlog" },
  { key: "ready", title: "Ready" },
  { key: "in_progress", title: "In Progress" },
  { key: "blocked", title: "Blocked" },
  { key: "review", title: "Review" },
  { key: "done", title: "Done" },
];

export const CANONICAL_BOARD_COLUMN_KEYS = CANONICAL_BOARD_COLUMNS.map(
  (column) => column.key,
);

export function createEmptyBoardColumnCounts() {
  return CANONICAL_BOARD_COLUMNS.reduce((counts, column) => {
    counts[column.key] = 0;
    return counts;
  }, {});
}

export function boardColumnTitle(columnKey, columnSchema = []) {
  const configured = (columnSchema ?? []).find(
    (column) => column?.key === columnKey,
  );
  if (configured?.title) {
    return configured.title;
  }

  const canonical = CANONICAL_BOARD_COLUMNS.find(
    (column) => column.key === columnKey,
  );
  return canonical?.title ?? columnKey;
}

export function boardSummaryCounts(summary) {
  const counts = createEmptyBoardColumnCounts();

  for (const [columnKey, count] of Object.entries(
    summary?.cards_by_column ?? {},
  )) {
    counts[columnKey] = Number(count ?? 0);
  }

  return counts;
}

export function groupBoardWorkspaceCards(cardsSection, columnSchema = []) {
  const groups = (columnSchema?.length ? columnSchema : CANONICAL_BOARD_COLUMNS)
    .map((column) => column.key)
    .reduce((acc, columnKey) => {
      acc[columnKey] = [];
      return acc;
    }, {});

  for (const item of cardsSection?.items ?? []) {
    const columnKey = String(item?.card?.column_key ?? "").trim();
    if (!groups[columnKey]) {
      groups[columnKey] = [];
    }
    groups[columnKey].push(item);
  }

  return groups;
}

export function parseDelimitedValues(rawValue) {
  const seen = new Set();
  const values = [];

  for (const item of String(rawValue ?? "").split(/\r?\n|,/)) {
    const value = item.trim();
    if (!value || seen.has(value)) {
      continue;
    }
    seen.add(value);
    values.push(value);
  }

  return values;
}

export function joinDelimitedValues(items) {
  return (items ?? [])
    .map((item) => String(item ?? "").trim())
    .filter(Boolean)
    .join("\n");
}
