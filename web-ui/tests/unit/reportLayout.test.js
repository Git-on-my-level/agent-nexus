import { describe, expect, it } from "vitest";
import {
  layoutContainsPanel,
  layoutHasVisiblePanels,
  layoutPanelIds,
  layoutSpanClass,
  selectedLayoutTab,
  visibleLayoutTabs,
} from "../../src/lib/components/reports/reportLayout.js";

const panel = (panel_id, extra = {}) => ({ type: "panel", panel_id, ...extra });
const tabs = {
  type: "tabs",
  id: "detail",
  items: [
    { id: "release", label: "Release", children: [panel("release")] },
    {
      id: "operations",
      label: "Operations",
      children: [
        {
          type: "disclosure",
          title: "Inspect evidence",
          children: [panel("proof"), panel("metrics")],
        },
      ],
    },
  ],
};
const layout = {
  type: "stack",
  children: [
    {
      type: "section",
      title: "Overview",
      children: [
        {
          type: "grid",
          columns: 3,
          children: [panel("summary", { span: 2 }), panel("risk")],
        },
      ],
    },
    tabs,
  ],
};

const visible = (...ids) => new Map(ids.map((id) => [id, { id }]));

describe("report composition", () => {
  it("collects all nested panel references without changing report order", () => {
    expect([...layoutPanelIds(layout)]).toEqual([
      "summary",
      "risk",
      "release",
      "proof",
      "metrics",
    ]);
    expect([...layoutPanelIds(undefined)]).toEqual([]);
    const panels = ["summary", "extra", "metrics", "another"];
    const referenced = layoutPanelIds(layout);
    expect(panels.filter((id) => !referenced.has(id))).toEqual([
      "extra",
      "another",
    ]);
  });

  it("finds evidence in nested tabs and disclosures", () => {
    expect(layoutContainsPanel(layout, "proof")).toBe(true);
    expect(layoutContainsPanel(layout, "missing")).toBe(false);
    expect(layoutContainsPanel(layout, "")).toBe(false);
  });

  it("hides every empty container after panel filtering", () => {
    expect(layoutHasVisiblePanels(layout, visible())).toBe(false);
    expect(layoutHasVisiblePanels(layout, visible("proof"))).toBe(true);
    expect(layoutHasVisiblePanels(layout.children[0], visible("proof"))).toBe(
      false,
    );
    expect(layoutHasVisiblePanels(layout.children[0], visible("summary"))).toBe(
      true,
    );
  });

  it("removes empty tabs while preserving authored order", () => {
    expect(
      visibleLayoutTabs(tabs, visible("metrics")).map((item) => item.id),
    ).toEqual(["operations"]);
    expect(
      visibleLayoutTabs(tabs, visible("release", "metrics")).map(
        (item) => item.id,
      ),
    ).toEqual(["release", "operations"]);
    expect(visibleLayoutTabs(tabs, visible())).toEqual([]);
  });

  it("respects a valid URL selection before evidence or default selection", () => {
    const items = visibleLayoutTabs(tabs, visible("release", "proof"));
    expect(selectedLayoutTab(items, "release", "proof")?.id).toBe("release");
    expect(selectedLayoutTab(items, "operations")?.id).toBe("operations");
    expect(selectedLayoutTab(items, "invalid", "proof")?.id).toBe("operations");
    expect(selectedLayoutTab(items, null, "proof")?.id).toBe("operations");
    expect(selectedLayoutTab(items, "invalid")?.id).toBe("release");
  });

  it("falls back when the URL-selected tab has no filtered panels", () => {
    const items = visibleLayoutTabs(tabs, visible("proof"));
    expect(selectedLayoutTab(items, "release")?.id).toBe("operations");
    expect(selectedLayoutTab([], "release", "proof")).toBeNull();
  });

  it("bounds column spans to the parent grid with a safe default", () => {
    expect(layoutSpanClass(undefined, 3)).toBe("layout-span-1");
    expect(layoutSpanClass(2, 3)).toBe("layout-span-2");
    expect(layoutSpanClass(4, 2)).toBe("layout-span-2");
    expect(layoutSpanClass(4, 4)).toBe("layout-span-4");
    expect(layoutSpanClass(99, 4)).toBe("layout-span-4");
    expect(layoutSpanClass(-1, 3)).toBe("layout-span-1");
  });
});
