// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";
import { withLiveObservation } from "../../src/lib/liveReports.js";
import { validateSeriesBinding } from "../../src/lib/seriesReports.js";
import { getPanelFreshness } from "../../src/lib/visualReports.js";
import SeriesReportPanel from "../../src/lib/components/reports/SeriesReportPanel.svelte";

afterEach(cleanup);

const pivotPanel = () => ({
  id: "loops",
  type: "live-table",
  title: "Loop status",
  data: {},
  observed_at: null,
  freshness: "unknown",
  source: {
    columns: [
      { series: "loops.state", label: "State" },
      { series: "loops.runs_7d", label: "Runs", agg: "sum" },
    ],
    row_keys: [{ key: "loop" }, { key: "host", label: "Host" }],
    range: "7d",
  },
});

const pivotObservation = () => ({
  status: "ok",
  observed_at: "2026-10-05T00:00:00Z",
  provenance: [
    {
      series: "loops.state",
      adapter: "collector",
      host: "m5-mbp",
      last_push: "2026-10-04T23:59:00Z",
      expected_interval_seconds: 60,
      resolution: "raw",
    },
    {
      series: "loops.runs_7d",
      adapter: "collector",
      host: "m5-mbp",
      last_push: "2026-10-04T23:59:00Z",
      expected_interval_seconds: 60,
      resolution: "raw",
    },
  ],
  data: {
    columns: [
      { label: "loop", kind: "key" },
      { label: "Host", kind: "key" },
      { label: "State", kind: "series", series: "loops.state", agg: "last" },
      { label: "Runs", kind: "series", series: "loops.runs_7d", agg: "sum" },
    ],
    rows: [
      {
        cells: [
          { text: "alpha" },
          { text: "m5-mbp" },
          {
            text: "running",
            observed_at: "2026-10-04T23:59:00Z",
            stale: false,
          },
          { text: "", observed_at: null, stale: false },
        ],
        source_ids: [],
      },
      {
        cells: [
          { text: "beta" },
          { text: "m5-mbp" },
          {
            text: "retrying",
            observed_at: "2026-10-04T12:00:00Z",
            stale: true,
          },
          { text: "12", observed_at: "2026-10-04T23:59:00Z", stale: false },
        ],
        source_ids: [],
      },
    ],
    more: 3,
  },
});

describe("live-table bindings", () => {
  it("accepts a pivoted source", () => {
    expect(validateSeriesBinding(pivotPanel())).toEqual([]);
  });
  it("rejects the single-series shape and pivot-only fields elsewhere", () => {
    expect(
      validateSeriesBinding({
        ...pivotPanel(),
        source: { series: "loops.state", row_keys: [{ key: "loop" }] },
      }),
    ).not.toEqual([]);
    expect(
      validateSeriesBinding({
        ...pivotPanel(),
        source: {
          columns: [{ series: "loops.state" }],
          row_keys: [{ key: "loop" }],
          agg: "sum",
        },
      }),
    ).not.toEqual([]);
    expect(
      validateSeriesBinding({
        type: "table",
        data: {},
        source: { series: "builds", row_keys: [{ key: "loop" }] },
      }),
    ).not.toEqual([]);
  });
  it("requires row keys and bounds the columns", () => {
    expect(
      validateSeriesBinding({
        ...pivotPanel(),
        source: { columns: [{ series: "loops.state" }] },
      }),
    ).not.toEqual([]);
    expect(
      validateSeriesBinding({
        ...pivotPanel(),
        source: {
          columns: [{ series: "loops.state" }],
          row_keys: [{ key: "loop" }, { key: "loop" }],
        },
      }),
    ).not.toEqual([]);
    expect(
      validateSeriesBinding({
        ...pivotPanel(),
        source: {
          columns: Array.from({ length: 9 }, (_, i) => ({
            series: `loops.m${i}`,
          })),
          row_keys: [{ key: "loop" }],
        },
      }),
    ).not.toEqual([]);
    expect(
      validateSeriesBinding({
        ...pivotPanel(),
        source: {
          columns: [{ series: "loops.state", label: "" }],
          row_keys: [{ key: "loop" }],
        },
      }),
    ).not.toEqual([]);
  });
});

describe("live-table observations", () => {
  it("merges freshness from the observation like other series panels", () => {
    const panel = withLiveObservation(pivotPanel(), pivotObservation());
    expect(panel.freshness).toBe("current");
    expect(getPanelFreshness(panel)).toBe("current");
    const stale = withLiveObservation(pivotPanel(), {
      ...pivotObservation(),
      status: "stale",
      stale_since: "2026-10-05T00:02:00Z",
    });
    expect(stale.freshness).toBe("stale");
    expect(getPanelFreshness(stale)).toBe("stale");
  });
  it("renders one row per label set with per-cell staleness and the more marker", () => {
    const panel = withLiveObservation(pivotPanel(), pivotObservation());
    render(SeriesReportPanel, { panel, freshness: "current" });
    expect(screen.getByText("running")).toBeTruthy();
    expect(screen.getByText("retrying")).toBeTruthy();
    // A missing observation reads as an em dash, never zero.
    expect(screen.getAllByText("—").length).toBe(1);
    expect(screen.getByText("stale")).toBeTruthy();
    expect(screen.getByText(/3 more rows not shown/)).toBeTruthy();
    expect(screen.getByText("loops.state · loops.runs_7d")).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: /Where this number comes from/ })
        .getAttribute("data-tooltip"),
    ).toMatch(/loops\.state is pushed by collector on m5-mbp/);
  });
  it("shows the materializer's reason when the read fails", () => {
    const panel = withLiveObservation(pivotPanel(), {
      status: "unavailable",
      message: "Series loops.missing is not declared.",
      data: {},
    });
    render(SeriesReportPanel, { panel, freshness: "unavailable" });
    expect(
      screen.getByText("Series loops.missing is not declared."),
    ).toBeTruthy();
  });
  it("re-sorts rows from the column headers", async () => {
    const panel = withLiveObservation(pivotPanel(), pivotObservation());
    render(SeriesReportPanel, { panel, freshness: "current" });
    const rowsInOrder = () =>
      screen
        .getAllByRole("row")
        .slice(1)
        .map((row) => row.textContent);
    expect(rowsInOrder()[0]).toMatch(/^alpha/);
    await fireEvent.click(screen.getByRole("button", { name: "Sort by loop" }));
    expect(rowsInOrder()[0]).toMatch(/^alpha/);
    await fireEvent.click(screen.getByRole("button", { name: "Sort by loop" }));
    expect(rowsInOrder()[0]).toMatch(/^beta/);
  });
});
