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
