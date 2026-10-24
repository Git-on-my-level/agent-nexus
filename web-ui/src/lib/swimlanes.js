/**
 * The swimlane board: one lane per top-level initiative, its children in
 * state columns. Presentation only. Core computes the hierarchy, the rollup
 * counts, likely homes and fold suggestions; nothing here infers a parent.
 *
 * Lanes come from the brief's `initiatives` (top-level, non-loop), each with
 * the rollup core returned with it, so the board never reads per lane.
 */

/**
 * Columns, left to right, and the stored phases each one holds. A child
 * waiting on the reader sits in `stuck` whatever its phase. Phase names stay
 * owned by `pm/presentation.js`; these are column headings.
 */
export const LANE_COLUMNS = [
  { key: "not_started", label: "Not started", phases: ["backlog", "ready"] },
  { key: "moving", label: "In progress", phases: ["in_progress", "review"] },
  { key: "stuck", label: "Blocked or needs you", phases: ["blocked"] },
  { key: "done", label: "Done", phases: ["done", "cancelled"] },
];

/** Cards a cell shows before it offers the rest. */
export const CELL_PREVIEW = 5;

/** The column a phase belongs in. An unknown phase has not started. */
export const phaseColumn = (phase) =>
  LANE_COLUMNS.find((column) => column.phases.includes(phase))?.key ??
  "not_started";

export function columnFor(child) {
  return child?.needsViewer ? "stuck" : phaseColumn(child?.state);
}

/** Children per column, in core's order, needs-you first within `stuck`. */
export function laneCells(rollup) {
  const cells = Object.fromEntries(LANE_COLUMNS.map(({ key }) => [key, []]));
  for (const child of rollup?.children ?? [])
    cells[columnFor(child)].push(child);
  cells.stuck.sort((a, b) => Number(b.needsViewer) - Number(a.needsViewer));
  return cells;
}

/**
 * The progress bar: one segment per column with children, sized by core's
 * `by_state` counts, done first so the bar fills from the left.
 */
export function progressSegments(byState) {
  const totals = Object.fromEntries(LANE_COLUMNS.map(({ key }) => [key, 0]));
  for (const [phase, count] of Object.entries(byState ?? {}))
    totals[phaseColumn(phase)] += Math.max(0, Number(count) || 0);
  const total = Object.values(totals).reduce((sum, n) => sum + n, 0);
  return ["done", "moving", "stuck", "not_started"]
    .filter((key) => totals[key] > 0)
    .map((key) => ({
      key,
      count: totals[key],
      share: totals[key] / total,
    }));
}

/**
 * Lanes in reading order: what needs the reader, then what is past its
 * promise, then core's own order, with finished initiatives last. Children and loops never become lanes,
 * even from a core that sent them here.
 */
export function buildLanes(initiatives) {
  return (initiatives ?? [])
    .filter((row) => !row.parentRef && !row.loop)
    .map((row, index) => ({ row, index }))
    .sort(
      (a, b) =>
        Number(a.row.state === "done") - Number(b.row.state === "done") ||
        b.row.rollup.needingViewer - a.row.rollup.needingViewer ||
        Number(b.row.state === "past_promise") -
          Number(a.row.state === "past_promise") ||
        b.row.rollup.pastPromise - a.row.rollup.pastPromise ||
        a.index - b.index,
    )
    .map(({ row }) => row);
}

/** Children core knows of that the preview does not carry. */
export function hiddenChildren(rollup) {
  return Math.max(0, (rollup?.total ?? 0) - (rollup?.children?.length ?? 0));
}

/** Why core suggested a home, in the reader's words. */
export const HOME_REASONS = {
  related_parent: "named as related",
  shared_ref: "shares a link",
  same_repository: "same repository",
  same_project: "same project",
  title_family: "same title prefix",
  stale_top_level: "quiet for a week",
};

export const homeReason = (reasons) =>
  (reasons ?? [])
    .map((reason) => HOME_REASONS[reason])
    .filter(Boolean)
    .join(", ");

/** `GET /work/ungrouped` rows, kept to what the tray shows. */
export function trayRows(raw) {
  return (Array.isArray(raw?.work) ? raw.work : [])
    .filter((row) => row?.ref && !row.parent_ref)
    .map((row) => ({
      ref: String(row.ref),
      title: String(row.title || row.ref),
      phase: String(row.phase || ""),
      createdAt: String(row.created_at || ""),
      homes: (Array.isArray(row.likely_home) ? row.likely_home : [])
        .filter((home) => home?.ref && home.ref !== row.ref)
        .map((home) => ({
          ref: String(home.ref),
          title: String(home.title || home.ref),
          reasons: Array.isArray(home.reasons) ? home.reasons : [],
        })),
    }));
}

/**
 * Fold suggestions from the brief's `next_actions`. Core sends them as argv
 * (`anx work fold <ref> --into <parent>`); anything else is not a fold.
 */
export function foldSuggestions(nextActions, initiatives = []) {
  const titles = new Map(
    (initiatives ?? []).map((row) => [row.ref, row.title]),
  );
  const seen = new Set();
  const out = [];
  for (const action of nextActions ?? []) {
    const argv = action?.argv ?? [];
    const at = argv.indexOf("fold");
    const into = argv.indexOf("--into");
    if (argv[0] !== "anx" || argv[1] !== "work" || at !== 2 || into < 0)
      continue;
    const ref = argv[at + 1];
    const parent = argv[into + 1];
    if (!ref || !parent || ref === parent || seen.has(ref)) continue;
    seen.add(ref);
    out.push({
      ref,
      title: titles.get(ref) || ref,
      into: parent,
      intoTitle: titles.get(parent) || parent,
      reason: homeReason(action.reasons),
    });
  }
  return out;
}

/** "Initiatives 17/15", or "17+/15" when the count is a lower bound. */
export function budgetLabel(budget) {
  if (!budget) return "";
  return `Initiatives ${budget.count}${budget.truncated ? "+" : ""}/${budget.target}`;
}

export const overBudget = (budget) =>
  Boolean(budget) && budget.count > budget.target;
