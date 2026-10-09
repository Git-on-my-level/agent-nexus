// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { page } from "$app/stores";
import { goto } from "$app/navigation";
import ReportLayout from "../../src/lib/components/reports/ReportLayout.svelte";
import VisualReport from "../../src/lib/components/reports/VisualReport.svelte";

vi.mock("$app/stores", async () => {
  const { writable } = await import("svelte/store");
  return { page: writable({ url: new URL("http://localhost/report") }) };
});
vi.mock("$app/navigation", () => ({ goto: vi.fn() }));

const now = Date.parse("2026-10-03T07:00:00Z");
const panel = (id, project_id = "release") => ({
  id,
  project_id,
  type: "explanation",
  title: `${id} panel`,
  author: "Report author",
  provenance: "reported",
  observed_at: "2026-10-03T07:00:00Z",
  freshness: "current",
  source_ids: [],
  data: { text: `${id} body` },
});
const ref = (panel_id, extra = {}) => ({ type: "panel", panel_id, ...extra });
const panels = [
  panel("overview"),
  panel("detail", "operations"),
  panel("extra"),
];
const tabs = {
  type: "tabs",
  id: "views",
  items: [
    { id: "all", label: "Overview", children: [ref("overview")] },
    { id: "detail", label: "Details", children: [ref("detail")] },
  ],
};
const props = (node, extra = {}) => ({
  node,
  panelsById: new Map(panels.map((item) => [item.id, item])),
  sources: [],
  now,
  tabSelections: new Map(),
  oninspect: vi.fn(),
  ontab: vi.fn(),
  ...extra,
});
const report = (layout) => ({
  schema_version: 1,
  title: "Release report",
  summary: "Evidence and outcomes",
  generated_at: "2026-10-03T07:00:00Z",
  projects: [
    {
      id: "release",
      title: "Release",
      outcome: "Published",
      summary: "Release facts",
    },
    {
      id: "operations",
      title: "Operations",
      outcome: "Pending",
      summary: "Operational facts",
    },
  ],
  panels,
  sources: [],
  ...(layout ? { layout } : {}),
});

beforeEach(() => {
  page.set({ url: new URL("http://localhost/report") });
  vi.clearAllMocks();
  goto.mockImplementation(async (url) => {
    page.set({ url: new URL(url) });
  });
});
afterEach(cleanup);

describe("ReportLayout rendering", () => {
  it("places panels by content when no column count is authored", () => {
    // A plan graph next to a nearly empty panel is the case that used to
    // render as two half-width columns with the graph clipped.
    const plan = {
      ...panel("plan"),
      type: "live-initiatives",
      data: { limit: 5 },
      live: {
        status: "ok",
        data: {
          items: [
            {
              ref: "card:a",
              plan: { steps: [{ id: "one" }, { id: "two" }] },
            },
          ],
        },
      },
    };
    const empty = {
      ...panel("asks"),
      type: "live-asks",
      data: { limit: 5 },
      live: { status: "ok", data: { items: [] } },
    };
    const { container } = render(
      ReportLayout,
      props(
        {
          type: "grid",
          children: [ref("plan"), ref("asks"), ref("overview")],
        },
        {
          panelsById: new Map([
            ["plan", plan],
            ["asks", empty],
            ["overview", panels[0]],
          ]),
        },
      ),
    );
    const grid = container.querySelector('[data-report-layout="grid"]');
    expect(grid.dataset.reportPlacement).toBe("fit");
    expect(grid.dataset.reportColumns).toBe("2");
    // The row's minimum comes from the plan's neighbours, not from the plan:
    // the wide panel has the row to itself, so it does not set the tier, and
    // the empty one never raises it.
    expect(grid.dataset.reportFit).toBe("tight");
    expect(
      [...grid.querySelectorAll(".report-layout-cell")].map(
        (cell) => cell.dataset.reportCell,
      ),
    ).toEqual(["full", "column", "column"]);
  });

  it("keeps an authored column count as a cap and still fits content", () => {
    const { container } = render(
      ReportLayout,
      props({
        type: "grid",
        columns: 3,
        children: [ref("overview"), ref("detail")],
      }),
    );
    const grid = container.querySelector('[data-report-layout="grid"]');
    expect(grid.dataset.reportPlacement).toBe("fit");
    expect(grid.dataset.reportColumns).toBe("3");
    expect(grid.querySelector(".layout-span-1")).toBeNull();
  });

  it("renders asymmetric spans in authored DOM order", () => {
    const { container } = render(
      ReportLayout,
      props({
        type: "section",
        title: "An editorial section",
        description: "Its supporting context",
        children: [
          {
            type: "grid",
            columns: 3,
            children: [ref("overview", { span: 2 }), ref("detail")],
          },
        ],
      }),
    );
    expect(
      screen.getByRole("heading", { name: "An editorial section" }),
    ).toBeTruthy();
    expect(screen.getByText("Its supporting context")).toBeTruthy();
    expect(
      [...container.querySelectorAll("[data-report-panel]")].map(
        (item) => item.dataset.reportPanel,
      ),
    ).toEqual(["overview", "detail"]);
    expect(
      container.querySelector(".layout-span-2 [data-report-panel]")?.dataset
        .reportPanel,
    ).toBe("overview");
    expect(
      container.querySelector('[data-report-layout="grid"]').dataset
        .reportPlacement,
    ).toBe("authored");
  });

  it("mounts only active tab content with connected accessible IDs", async () => {
    const { container, rerender } = render(ReportLayout, props(tabs));
    const first = screen.getByRole("tab", { name: "Overview" });
    const second = screen.getByRole("tab", { name: "Details" });
    expect(first.getAttribute("aria-selected")).toBe("true");
    expect(second.tabIndex).toBe(-1);
    expect(
      document
        .getElementById(first.getAttribute("aria-controls"))
        ?.getAttribute("aria-labelledby"),
    ).toBe(first.id);
    expect(container.querySelector('[data-report-panel="detail"]')).toBeNull();
    await rerender({ tabSelections: new Map([["views", "detail"]]) });
    expect(second.getAttribute("aria-selected")).toBe("true");
    expect(
      container.querySelector('[data-report-panel="overview"]'),
    ).toBeNull();
    expect(
      container.querySelector('[data-report-panel="detail"]'),
    ).toBeTruthy();
  });

  it("supports arrow wrap, Home, and End with roving focus", async () => {
    const ontab = vi.fn();
    render(ReportLayout, props(tabs, { ontab }));
    const first = screen.getByRole("tab", { name: "Overview" });
    const second = screen.getByRole("tab", { name: "Details" });
    first.focus();
    await fireEvent.keyDown(first, { key: "ArrowLeft" });
    expect(document.activeElement).toBe(second);
    expect(ontab).toHaveBeenLastCalledWith("views", "detail");
    await fireEvent.keyDown(second, { key: "ArrowRight" });
    expect(document.activeElement).toBe(first);
    await fireEvent.keyDown(first, { key: "End" });
    expect(document.activeElement).toBe(second);
    await fireEvent.keyDown(second, { key: "Home" });
    expect(document.activeElement).toBe(first);
  });

  it("removes filtered tab choices and all empty section/disclosure shells", async () => {
    const { container, rerender } = render(
      ReportLayout,
      props(
        {
          type: "stack",
          children: [
            {
              type: "section",
              title: "Filtered section",
              children: [ref("overview")],
            },
            {
              type: "disclosure",
              title: "Filtered disclosure",
              children: [ref("overview")],
            },
            tabs,
          ],
        },
        { panelsById: new Map([["detail", panels[1]]]) },
      ),
    );
    expect(
      screen.queryByRole("heading", { name: "Filtered section" }),
    ).toBeNull();
    expect(container.querySelector("details")).toBeNull();
    expect(screen.queryByRole("tab", { name: "Overview" })).toBeNull();
    expect(
      screen
        .getByRole("tab", { name: "Details" })
        .getAttribute("aria-selected"),
    ).toBe("true");
    await rerender({ panelsById: new Map() });
    expect(container.querySelector("[data-report-layout]")).toBeNull();
  });

  it("lazily mounts disclosures and opens them for a new evidence link", async () => {
    const { container, rerender } = render(
      ReportLayout,
      props({
        type: "disclosure",
        title: "More evidence",
        children: [ref("overview")],
      }),
    );
    const disclosure = container.querySelector("details");
    expect(disclosure.open).toBe(false);
    expect(container.querySelector("[data-report-panel]")).toBeNull();
    disclosure.open = true;
    await fireEvent(disclosure, new Event("toggle"));
    expect(container.querySelector("[data-report-panel]")).toBeTruthy();
    disclosure.open = false;
    await fireEvent(disclosure, new Event("toggle"));
    expect(container.querySelector("[data-report-panel]")).toBeNull();
    await rerender({ evidence: "overview" });
    expect(disclosure.open).toBe(true);
    expect(
      screen
        .getByRole("button", { name: "Inspect evidence" })
        .getAttribute("aria-expanded"),
    ).toBe("true");
  });
});

describe("VisualReport composition compatibility", () => {
  it("retains every legacy panel when layout is absent", () => {
    const { container } = render(VisualReport, { report: report() });
    expect(
      [...container.querySelectorAll("[data-report-panel]")].map(
        (item) => item.dataset.reportPanel,
      ),
    ).toEqual(["overview", "detail", "extra"]);
    // And places them through the same content-driven grid an authored
    // layout uses, rather than a second hard-coded two-column path.
    expect(
      container.querySelector('[data-report-layout="grid"]').dataset
        .reportPlacement,
    ).toBe("fit");
  });

  it("compacts single-project compositions without changing the legacy overview", async () => {
    const single = report(ref("overview"));
    single.projects = single.projects.slice(0, 1);
    const { container, rerender } = render(VisualReport, { report: single });
    expect(
      container.querySelector('[aria-label="Project overview"]'),
    ).toBeNull();
    await rerender({ report: { ...single, layout: undefined } });
    expect(
      container.querySelector('[aria-label="Project overview"]'),
    ).toBeTruthy();
  });

  it("appends unreferenced panels in original order", () => {
    const { container } = render(VisualReport, {
      report: report(ref("detail")),
    });
    expect(
      [...container.querySelectorAll("[data-report-panel]")].map(
        (item) => item.dataset.reportPanel,
      ),
    ).toEqual(["detail", "overview", "extra"]);
  });

  it("writes tab IDs including all while preserving filters, evidence, and unrelated query state", async () => {
    page.set({
      url: new URL(
        "http://localhost/report?reportFreshness=all&reportEvidence=detail&other=kept&reportTab.views=detail",
      ),
    });
    render(VisualReport, { report: report(tabs) });
    await fireEvent.click(screen.getByRole("tab", { name: "Overview" }));
    const [url, options] = goto.mock.calls.at(-1);
    expect(url.searchParams.get("reportTab.views")).toBe("all");
    expect(url.searchParams.get("reportEvidence")).toBe("detail");
    expect(url.searchParams.get("reportFreshness")).toBe("all");
    expect(url.searchParams.get("other")).toBe("kept");
    expect(options).toEqual({ noScroll: true, keepFocus: true });
  });

  it("restores tab and evidence state from URL navigation", async () => {
    render(VisualReport, { report: report(tabs) });
    page.set({
      url: new URL(
        "http://localhost/report?reportTab.views=detail&reportEvidence=detail",
      ),
    });
    await tick();
    expect(
      screen
        .getByRole("tab", { name: "Details" })
        .getAttribute("aria-selected"),
    ).toBe("true");
    expect(
      screen
        .getAllByRole("button", { name: "Inspect evidence" })[0]
        .getAttribute("aria-expanded"),
    ).toBe("true");
    page.set({ url: new URL("http://localhost/report?reportTab.views=all") });
    await tick();
    expect(
      screen
        .getByRole("tab", { name: "Overview" })
        .getAttribute("aria-selected"),
    ).toBe("true");
    expect(
      screen
        .getAllByRole("button", { name: "Inspect evidence" })[0]
        .getAttribute("aria-expanded"),
    ).toBe("false");
  });

  it("retains tab selection when project filters change and clears evidence", async () => {
    page.set({
      url: new URL(
        "http://localhost/report?reportTab.views=detail&reportEvidence=detail",
      ),
    });
    render(VisualReport, { report: report(tabs) });
    await fireEvent.click(
      screen.getByRole("button", { name: /Release.*Published/ }),
    );
    const [url] = goto.mock.calls.at(-1);
    expect(url.searchParams.get("reportProject")).toBe("release");
    expect(url.searchParams.get("reportTab.views")).toBe("detail");
    expect(url.searchParams.has("reportEvidence")).toBe(false);
    expect(screen.queryByRole("tab", { name: "Details" })).toBeNull();
    expect(
      screen
        .getByRole("tab", { name: "Overview" })
        .getAttribute("aria-selected"),
    ).toBe("true");
  });
});
