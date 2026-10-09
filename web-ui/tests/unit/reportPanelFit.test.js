import { describe, expect, it } from "vitest";

import {
  PANEL_FIT_WIDTH,
  panelFit,
} from "../../src/lib/components/reports/panelFit.js";
import {
  createAutoGrid,
  gridColumns,
  gridPlacement,
  layoutFit,
} from "../../src/lib/components/reports/reportLayout.js";

const panel = (type, data, extra = {}) => ({
  id: `${type}-panel`,
  project_id: "p",
  type,
  title: type,
  author: "author",
  provenance: "reported",
  observed_at: "2026-10-09T07:00:00Z",
  freshness: "current",
  source_ids: [],
  data,
  ...extra,
});
const live = (type, status, data) =>
  panel(type, { limit: 5 }, { live: { status, data } });
const prose = (length) =>
  "word ".repeat(Math.ceil(length / 5)).slice(0, length);

describe("panel fit reads content, not just type", () => {
  it("gives prose a column and a label-length panel the narrow tier", () => {
    expect(panelFit(panel("explanation", { text: prose(400) }))).toMatchObject({
      tier: "text",
      wide: false,
      sparse: false,
      min: PANEL_FIT_WIDTH.text,
    });
    expect(panelFit(panel("explanation", { text: "Shipped." }))).toMatchObject({
      tier: "tight",
      sparse: true,
      wide: false,
    });
  });

  it("claims the full row for a plan graph and not for a planless list", () => {
    const withPlan = live("live-initiatives", "ok", {
      items: [
        {
          ref: "card:a",
          plan: { steps: [{ id: "one" }, { id: "two" }] },
        },
      ],
    });
    const withoutPlan = live("live-initiatives", "ok", {
      items: [{ ref: "card:a" }, { ref: "card:b" }],
    });
    expect(panelFit(withPlan)).toMatchObject({
      wide: true,
      tier: "wide",
      min: PANEL_FIT_WIDTH.wide,
    });
    expect(panelFit(withoutPlan)).toMatchObject({ wide: false, tier: "text" });
  });

  it("treats a wide type with little in it as narrow, and empty as sparse", () => {
    const small = panel("table", {
      columns: ["Claim", "Observation"],
      rows: [{ cells: ["a", "b"], source_ids: [] }],
    });
    const real = panel("table", {
      columns: ["One", "Two", "Three", "Four"],
      rows: Array.from({ length: 9 }, () => ({
        cells: ["a", "b", "c", "d"],
        source_ids: [],
      })),
    });
    const empty = panel("table", { columns: ["Claim"], rows: [] });
    expect(panelFit(small)).toMatchObject({ wide: false, tier: "text" });
    expect(panelFit(real)).toMatchObject({ wide: true, tier: "wide" });
    expect(panelFit(empty)).toMatchObject({
      wide: false,
      sparse: true,
      tier: "tight",
    });
  });

  it("keeps a live panel's type placement while it is still reading", () => {
    const loading = live("live-initiatives", "loading", {});
    const failed = live("live-initiatives", "error", {});
    const answered = live("live-initiatives", "ok", { items: [] });
    // Waiting is not emptiness: placement must not change under the reader
    // when the observation lands.
    expect(panelFit(loading)).toMatchObject({ sparse: false, wide: true });
    expect(panelFit(failed)).toMatchObject({ sparse: false, wide: true });
    expect(panelFit(answered)).toMatchObject({ sparse: true, wide: false });
  });

  it("keeps a nearly empty panel out of the row's minimum", () => {
    const asks = live("live-asks", "ok", { items: [] });
    const notes = panel("explanation", { text: prose(400) });
    const panelsById = new Map([
      [asks.id, asks],
      [notes.id, notes],
    ]);
    const node = { type: "grid" };
    const children = [
      { type: "panel", panel_id: asks.id },
      { type: "panel", panel_id: notes.id },
    ];
    // The empty panel would read fine in 260px; the prose next to it decides
    // the row, and the empty one cannot drag the row down to its own tier.
    expect(gridPlacement(node, children, panelsById).tier).toBe("text");
    expect(gridPlacement(node, [children[0]], panelsById).tier).toBe("tight");
  });
});

describe("grid placement", () => {
  const wide = live("live-initiatives", "ok", {
    items: [{ ref: "card:a", plan: { steps: [{ id: "one" }, { id: "two" }] } }],
  });
  const note = panel("callout", { tone: "info", text: prose(400) });
  const panelsById = new Map([
    [wide.id, wide],
    [note.id, note],
  ]);
  const children = [
    { type: "panel", panel_id: wide.id },
    { type: "panel", panel_id: note.id },
  ];

  it("defaults to a content-driven grid capped at two columns", () => {
    const placement = gridPlacement({ type: "grid" }, children, panelsById);
    expect(placement).toMatchObject({ authored: false, columns: 2 });
    expect(placement.cells.map((cell) => cell.full)).toEqual([true, false]);
    expect(placement.cells.map((cell) => cell.spanClass)).toEqual(["", ""]);
  });

  it("honours a column cap without making it a requirement", () => {
    expect(gridColumns(undefined)).toBe(2);
    expect(gridColumns(3)).toBe(3);
    expect(gridColumns(5)).toBe(2);
    expect(
      gridPlacement({ type: "grid", columns: 4 }, children, panelsById).columns,
    ).toBe(4);
  });

  it("reads a span of two or more as full width when no columns are named", () => {
    const placement = gridPlacement(
      { type: "grid" },
      [{ type: "panel", panel_id: note.id, span: 2 }],
      panelsById,
    );
    expect(placement.cells[0].full).toBe(true);
  });

  it("keeps exact tracks and spans for an authored grid", () => {
    const placement = gridPlacement(
      { type: "grid", columns: 3 },
      [
        { type: "panel", panel_id: note.id, span: 2 },
        { type: "panel", panel_id: wide.id },
      ],
      panelsById,
    );
    expect(placement.authored).toBe(true);
    expect(placement.cells.map((cell) => cell.spanClass)).toEqual([
      "layout-span-2",
      "layout-span-1",
    ]);
    // Authored placement wins: the wide panel does not seize the row.
    expect(placement.cells.map((cell) => cell.full)).toEqual([false, false]);
  });

  it("measures a container cell by the widest panel inside it", () => {
    const fit = layoutFit(
      {
        type: "section",
        title: "Detail",
        children: [{ type: "panel", panel_id: wide.id }],
      },
      panelsById,
    );
    expect(fit).toEqual({ tier: "wide", full: true });
  });

  it("keeps one node per panel id so observations do not remount panels", () => {
    const autoGrid = createAutoGrid();
    const first = autoGrid([wide, note]);
    const second = autoGrid([{ ...wide }, { ...note }]);
    expect(first.children[0]).toBe(second.children[0]);
    expect(first.children.map((child) => child.panel_id)).toEqual([
      wide.id,
      note.id,
    ]);
  });
});
