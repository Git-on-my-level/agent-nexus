// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ReportChart from "../../src/lib/components/reports/ReportChart.svelte";
import { createReportChart } from "../../src/lib/components/reports/reportChartRenderer.js";

vi.mock("../../src/lib/components/reports/reportChartRenderer.js", () => ({
  createReportChart: vi.fn(),
}));
const data = () => ({
  caption: "Missing observations remain gaps.",
  option: {
    xAxis: { type: "category", data: ["Monday", "Tuesday"] },
    yAxis: { type: "value" },
    series: [{ type: "line", name: "Change", data: [-2, null] }],
  },
});
let instance;
let observed;
let disconnected;
let resizeCallback;
beforeEach(() => {
  vi.clearAllMocks();
  instance = { setOption: vi.fn(), resize: vi.fn(), dispose: vi.fn() };
  createReportChart.mockReturnValue(instance);
  observed = vi.fn();
  disconnected = vi.fn();
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback) {
        resizeCallback = callback;
      }
      observe(element) {
        observed(element);
      }
      disconnect() {
        disconnected();
      }
    },
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ReportChart", () => {
  it("renders an accessible caption and complete semantic data table", async () => {
    const { container } = render(ReportChart, {
      data: data(),
      title: "Weekly change",
    });
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    expect(container.querySelector("[data-report-chart]")).not.toBeNull();
    expect(screen.getByRole("img").getAttribute("aria-label")).toContain(
      "Weekly change",
    );
    expect(screen.getByText("Missing observations remain gaps.")).toBeTruthy();
    await fireEvent.click(screen.getByText("View chart data"));
    expect(
      screen.getByRole("region", { name: "Weekly change data" }).tabIndex,
    ).toBe(0);
    expect(screen.getByRole("table")).toBeTruthy();
    expect(screen.getByText("No observation")).toBeTruthy();
    expect(screen.getByText("-2")).toBeTruthy();
  });

  it("resizes with its container and disposes observers and renderer on unmount", async () => {
    const { unmount } = render(ReportChart, { data: data(), title: "Chart" });
    await waitFor(() => expect(observed).toHaveBeenCalledOnce());
    const element = observed.mock.calls[0][0];
    Object.defineProperty(element, "clientWidth", { value: 380 });
    Object.defineProperty(element, "clientHeight", { value: 280 });
    resizeCallback();
    expect(instance.resize).toHaveBeenCalledWith({ width: 380, height: 280 });
    unmount();
    expect(instance.dispose).toHaveBeenCalledOnce();
    expect(disconnected).toHaveBeenCalledOnce();
  });

  it("replaces chart state rather than merging old series on data changes", async () => {
    const { rerender } = render(ReportChart, { data: data(), title: "Chart" });
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const next = data();
    next.option.series[0].data = [4, 5];
    await rerender({ data: next, title: "New chart" });
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledTimes(2));
    expect(instance.dispose).toHaveBeenCalledOnce();
    expect(instance.setOption.mock.calls[1][0].series[0].data).toEqual([4, 5]);
    expect(instance.setOption.mock.calls[1][1]).toEqual({ notMerge: true });
  });

  it("opens the table and gives a useful fallback when SVG rendering fails", async () => {
    instance.setOption.mockImplementation(() => {
      throw new Error("unsupported renderer");
    });
    const { container } = render(ReportChart, { data: data(), title: "Chart" });
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain(
        "could not be displayed",
      ),
    );
    expect(container.querySelector("details").open).toBe(true);
    expect(screen.getByRole("table")).toBeTruthy();
    expect(instance.dispose).toHaveBeenCalledOnce();
  });

  it("does not initialize ECharts for invalid options", async () => {
    const invalid = data();
    invalid.option.tooltip = { formatter: "<script>" };
    render(ReportChart, { data: invalid, title: "Chart" });
    expect(
      screen.getByText("This chart uses unsupported or invalid data."),
    ).toBeTruthy();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(createReportChart).not.toHaveBeenCalled();
  });
});
