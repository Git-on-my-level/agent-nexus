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

  it("reads a series-bound panel the way its renderer does", () => {
    const rows = (count) => ({
      columns: ["One", "Two", "Three", "Four"],
      rows: Array.from({ length: count }, () => ({
        cells: ["a", "b", "c", "d"],
        source_ids: [],
      })),
    });
    const bound = (extra) => ({
      ...panel("table", {}),
      source: { series: "builds", range: "30d" },
      ...extra,
    });
    /*
     * `withSeriesObservation` blanks `panel.data` for any status but `ok`,
     * so measuring it while the read is in flight would call a full table
     * empty and then reflow the row when the rows arrive.
     */
    expect(
      panelFit(bound({ data: {}, seriesObservation: { status: "loading" } })),
    ).toMatchObject({ sparse: false, wide: true });
    expect(
      panelFit(bound({ data: {}, seriesObservation: { status: "error" } })),
    ).toMatchObject({ sparse: false, wide: true });
    expect(
      panelFit(bound({ data: rows(9), seriesObservation: { status: "ok" } })),
    ).toMatchObject({ wide: true, tier: "wide" });
    expect(
      panelFit(bound({ data: rows(0), seriesObservation: { status: "ok" } })),
    ).toMatchObject({ sparse: true, tier: "tight" });
    // A stale series still renders, from the observation, so it is measured
    // there too: an irregular stream is stale between events by definition.
    expect(
      panelFit(
        bound({
          ...panel("live-timeline", {}),
          source: { series: "releases", range: "30d" },
          data: {},
          seriesObservation: {
            status: "stale",
            data: { items: [{ at: "2026-10-01T00:00:00Z", value: 1 }] },
          },
        }),
      ),
    ).toMatchObject({ sparse: false, wide: true });
    // A fallback snapshot is on `panel.data`, and it is what the reader sees.
    expect(
      panelFit(
        bound({
          data: rows(9),
          seriesFallback: true,
          seriesObservation: { status: "unavailable" },
        }),
      ),
    ).toMatchObject({ wide: true });
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
  const other = { ...panel("explanation", { text: prose(400) }), id: "notes" };
  const panelsById = new Map([
    [wide.id, wide],
    [note.id, note],
    [other.id, other],
  ]);
  const children = [
    { type: "panel", panel_id: wide.id },
    { type: "panel", panel_id: note.id },
  ];
  const threeChildren = [...children, { type: "panel", panel_id: other.id }];

  it("defaults to a content-driven grid capped at two columns", () => {
    const placement = gridPlacement(
      { type: "grid" },
      threeChildren,
      panelsById,
    );
    expect(placement).toMatchObject({ authored: false, columns: 2 });
    expect(placement.cells.map((cell) => cell.full)).toEqual([
      true,
      false,
      false,
    ]);
    expect(placement.cells.map((cell) => cell.spanClass)).toEqual(["", "", ""]);
  });

  it("honours a column cap without making it a requirement", () => {
    expect(gridColumns(undefined)).toBe(2);
    expect(gridColumns(3)).toBe(3);
    expect(gridColumns(5)).toBe(2);
    // A stored grid that names columns but no spans is content-driven: its
    // count is the maximum, and narrower widths use fewer.
    const stored = gridPlacement(
      { type: "grid", columns: 4 },
      threeChildren,
      panelsById,
    );
    expect(stored).toMatchObject({ authored: false, columns: 4 });
  });

  it("splits groups at a full-row panel rather than counting the whole grid", () => {
    /*
     * prose, chart, prose is two groups of one, not one group of two: the
     * chart between them takes the row, so neither prose panel has anything
     * to share a row with. Counting leftovers across the whole grid saw two
     * narrow panels and left both at half width with a gap beside each.
     */
    const placement = gridPlacement(
      { type: "grid" },
      [
        { type: "panel", panel_id: note.id },
        { type: "panel", panel_id: wide.id },
        { type: "panel", panel_id: other.id },
      ],
      panelsById,
    );
    expect(placement.cells.map((cell) => cell.full)).toEqual([
      true,
      true,
      true,
    ]);
  });

  it("widens the odd one out of a group, at the default two-column cap", () => {
    const third = { ...panel("callout", { tone: "info", text: prose(400) }) };
    third.id = "third";
    const byId = new Map([...panelsById, [third.id, third]]);
    const group = (ids) => ids.map((id) => ({ type: "panel", panel_id: id }));
    // Three narrow panels at a cap of two: two share the first row, and the
    // third would sit alone on the second with a gap beside it.
    expect(
      gridPlacement(
        { type: "grid" },
        group([note.id, other.id, third.id]),
        byId,
      ).cells.map((cell) => cell.full),
    ).toEqual([false, false, true]);
    // An even group fills both of its rows, so nothing is widened.
    expect(
      gridPlacement(
        { type: "grid" },
        group([note.id, other.id]),
        byId,
      ).cells.map((cell) => cell.full),
    ).toEqual([false, false]);
    /*
     * A named cap of 3 or 4 can resolve to anything from one column up to it,
     * so only a group of one is decidable here; the grid lays out the rest.
     */
    expect(
      gridPlacement(
        { type: "grid", columns: 4 },
        group([note.id, other.id, third.id]),
        byId,
      ).cells.map((cell) => cell.full),
    ).toEqual([false, false, false]);
    expect(
      gridPlacement(
        { type: "grid", columns: 4 },
        [{ type: "panel", panel_id: wide.id }, ...group([note.id])],
        byId,
      ).cells.map((cell) => cell.full),
    ).toEqual([true, true]);
  });

  it("gives the row to a panel that would otherwise be left alone on it", () => {
    // auto-fit collapses tracks nothing is placed in, so a lone panel already
    // fills its width — unless a full-row neighbour keeps the tracks alive.
    const placement = gridPlacement({ type: "grid" }, children, panelsById);
    expect(placement.cells.map((cell) => cell.full)).toEqual([true, true]);
    const withTwoNarrow = gridPlacement(
      { type: "grid" },
      threeChildren,
      panelsById,
    );
    expect(withTwoNarrow.cells.map((cell) => cell.full)).toEqual([
      true,
      false,
      false,
    ]);
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
