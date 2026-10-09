import { PANEL_FIT_TIERS, panelFit, widerTier } from "./panelFit.js";

/**
 * Columns a grid may use at its widest. A grid that names none is capped at
 * two: the question a reader has on a laptop is never "why only two columns",
 * it is "why is this plan graph cut in half".
 */
export const DEFAULT_GRID_COLUMNS = 2;

/** Layout helpers operate only on validated, data-only report nodes. */
export function layoutPanelIds(node, result = new Set()) {
  if (!node) return result;
  if (node.type === "panel") result.add(node.panel_id);
  for (const child of node.children ?? []) layoutPanelIds(child, result);
  for (const item of node.items ?? []) {
    for (const child of item.children) layoutPanelIds(child, result);
  }
  return result;
}

export function layoutContainsPanel(node, panelId) {
  if (!panelId) return false;
  if (node.type === "panel") return node.panel_id === panelId;
  return (
    (node.children ?? []).some((child) =>
      layoutContainsPanel(child, panelId),
    ) ||
    (node.items ?? []).some((item) =>
      item.children.some((child) => layoutContainsPanel(child, panelId)),
    )
  );
}

export function layoutHasVisiblePanels(node, panelsById) {
  if (node.type === "panel") return panelsById.has(node.panel_id);
  return (
    (node.children ?? []).some((child) =>
      layoutHasVisiblePanels(child, panelsById),
    ) ||
    (node.items ?? []).some((item) =>
      item.children.some((child) => layoutHasVisiblePanels(child, panelsById)),
    )
  );
}

export function visibleLayoutTabs(node, panelsById) {
  return (node.items ?? []).filter((item) =>
    item.children.some((child) => layoutHasVisiblePanels(child, panelsById)),
  );
}

export function selectedLayoutTab(items, requestedId, evidenceId = "") {
  return (
    items.find((item) => item.id === requestedId) ??
    items.find((item) =>
      item.children.some((child) => layoutContainsPanel(child, evidenceId)),
    ) ??
    items[0] ??
    null
  );
}

export function layoutSpanClass(span, columns) {
  // The validator constrains both inputs; keep renderer CSS bounded as well.
  const count = Math.max(1, Math.min(Number(span) || 1, columns, 4));
  return {
    1: "layout-span-1",
    2: "layout-span-2",
    3: "layout-span-3",
    4: "layout-span-4",
  }[count];
}

/** A grid's column cap: what it asked for, or the responsive default. */
export function gridColumns(columns) {
  const count = Number(columns);
  return count === 2 || count === 3 || count === 4
    ? count
    : DEFAULT_GRID_COLUMNS;
}

/**
 * What a whole subtree needs from the row it sits in: the widest minimum among
 * the panels inside it, and whether any of them is wide enough to want the row
 * to itself. A section or a stack placed in a grid cell is as wide as the
 * widest thing in it.
 *
 * Deliberately blind to what is currently *shown*: a chart inside a closed
 * disclosure or an unselected tab still counts. Measuring only the visible
 * branch would move the panel beside it every time a reader opened a fold,
 * which is a worse answer than a collapsed summary line sitting on a row of
 * its own.
 */
export function layoutFit(node, panelsById) {
  if (!node || typeof node !== "object") return { tier: "tight", full: false };
  if (node.type === "panel") {
    const panel = panelsById.get(node.panel_id);
    if (!panel) return { tier: "tight", full: false };
    const fit = panelFit(panel);
    return { tier: fit.tier, full: fit.wide };
  }
  const children = [
    ...(node.children ?? []),
    ...(node.items ?? []).flatMap((item) => item.children ?? []),
  ];
  return children.reduce(
    (acc, child) => {
      const fit = layoutFit(child, panelsById);
      return {
        tier: widerTier(acc.tier, fit.tier),
        full: acc.full || fit.full,
      };
    },
    { tier: "tight", full: false },
  );
}

/**
 * How a grid's visible children are placed.
 *
 * Two paths, and which one a grid takes is decided by its own content:
 *
 * - **Content-driven** (the default, and what a grid with no `span` on any
 *   child gets): columns come from available width and each child's minimum
 *   useful width, capped at the grid's column count. Wide children take the
 *   full row.
 * - **Authored** (a grid where a child asks for a `span` of 2 or more): exact
 *   tracks and exact spans, as before, collapsing to one column on narrow
 *   screens. An author who places panels by hand gets what they placed.
 *
 * @param {object} node a validated `grid` layout node
 * @param {object[]} children its visible children, in order
 * @param {Map<string, object>} panelsById the panels still passing filters
 */
export function gridPlacement(node, children, panelsById) {
  const columns = gridColumns(node.columns);
  // Exact tracks need an exact column count to span. A grid that names no
  // columns has none, so a span there is the full-width override instead.
  const authored =
    node.columns !== undefined &&
    children.some((child) => Number(child.span) > 1);
  const cells = children.map((child) => {
    const fit = layoutFit(child, panelsById);
    return {
      // A span of 2 or more in a content-driven grid is the "full width"
      // override: there is no fixed track count for it to mean anything else.
      full: !authored && (fit.full || Number(child.span) > 1),
      spanClass: authored ? layoutSpanClass(child.span, columns) : "",
      tier: fit.tier,
    };
  });
  /*
   * A panel left alone on a row takes the row.
   *
   * `auto-fit` collapses tracks nothing is placed in, so narrow panels with
   * the grid to themselves already fill its width. A full-row panel defeats
   * that — the tracks stay occupied — and it also *splits* its neighbours:
   * `prose, chart, prose` is not one group of two narrow panels, it is two
   * groups of one, and each of those renders at half width with the other
   * half blank. So leftovers are worked out per contiguous group, in reading
   * order, never by reordering panels to fill a row.
   *
   * What a group leaves over depends on the column count, and only the
   * renderer knows that one — it reads it off the width available. Two cases
   * are decidable here:
   *
   * - A group of one is alone on its row at every column count.
   * - At the default cap the grid resolves to exactly two columns or one, so
   *   an odd-numbered group ends with a panel alone. Widening it is a no-op
   *   in the one-column case, where it already has the full width.
   *
   * A cap of 3 or 4 can resolve to anything from 1 up to it, so beyond a
   * group of one there is nothing to decide and the grid lays it out.
   */
  if (!authored) {
    let group = [];
    const settle = () => {
      const alone =
        group.length === 1 || (columns === 2 && group.length % 2 === 1);
      if (alone) group[group.length - 1].full = true;
      group = [];
    };
    for (const cell of cells) {
      if (cell.full) settle();
      else group.push(cell);
    }
    settle();
  }
  const tier = cells
    .filter((cell) => !cell.full)
    .reduce((acc, cell) => widerTier(acc, cell.tier), PANEL_FIT_TIERS[0]);
  return { authored, columns, tier, cells };
}

/**
 * A builder for the layout a report with no `layout` gets: every panel in one
 * content-driven grid, in stored order. Panels a layout did not reference are
 * appended the same way, so "don't specify layout" and "specify part of it"
 * land in exactly the same renderer.
 *
 * It is a builder rather than a function because the child nodes are the
 * renderer's `{#each}` keys. A live observation lands every refresh and
 * rebuilds the panel array; minting fresh child objects for it would remount
 * every panel underneath, so each panel id keeps one node for the life of the
 * report.
 */
export function createAutoGrid() {
  const nodes = new Map();
  return (panels) => ({
    type: "grid",
    children: panels.map((panel) => {
      let node = nodes.get(panel.id);
      if (!node) {
        node = { type: "panel", panel_id: panel.id };
        nodes.set(panel.id, node);
      }
      return node;
    }),
  });
}
