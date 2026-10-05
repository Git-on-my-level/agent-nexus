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
let handlers;
beforeEach(() => {
  vi.clearAllMocks();
  handlers = {};
  instance = {
    setOption: vi.fn(),
    resize: vi.fn(),
    dispose: vi.fn(),
    dispatchAction: vi.fn(),
    on: vi.fn((event, handler) => {
      handlers[event] = handler;
    }),
  };
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

describe("ReportChart legend and tooltip", () => {
  /** Two series, so the legend is not just repeating the panel title. */
  const twoSeries = () => ({
    option: {
      xAxis: { type: "category", data: ["Mon", "Tue"] },
      yAxis: { type: "value" },
      series: [
        { type: "line", name: "Opened", data: [2, 6] },
        { type: "line", name: "Closed", data: [1, 3] },
      ],
    },
  });

  const mount = (chartData = twoSeries()) =>
    render(ReportChart, { data: chartData, title: "Flow" });

  it("renders a wrapping legend of real buttons, one per series", async () => {
    const { container } = mount();
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const items = container.querySelectorAll(".chart-legend__item");
    expect(items).toHaveLength(2);
    expect(items[0].tagName).toBe("BUTTON");
    expect(items[0].textContent).toContain("Opened");
    expect(items[1].textContent).toContain("Closed");
    // Wrapping, not paginating: there is no pager control to find.
    expect(container.querySelector(".chart-legend")).not.toBeNull();
  });

  it("does not render a legend that would only repeat the panel title", async () => {
    const single = twoSeries();
    single.option.series = [single.option.series[0]];
    const { container } = mount(single);
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    expect(container.querySelector(".chart-legend")).toBeNull();
  });

  it("pins a series on click and unpins it on a second click", async () => {
    const { container } = mount();
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const [opened] = container.querySelectorAll(".chart-legend__item");

    expect(opened.getAttribute("aria-pressed")).toBe("false");
    await fireEvent.click(opened);
    expect(opened.getAttribute("aria-pressed")).toBe("true");
    await waitFor(() =>
      expect(instance.dispatchAction).toHaveBeenCalledWith({
        type: "highlight",
        seriesIndex: 0,
      }),
    );
    // The other series is muted, not hidden: no legendToggleSelect anywhere.
    expect(instance.dispatchAction).toHaveBeenCalledWith({
      type: "downplay",
      seriesIndex: 1,
    });

    instance.dispatchAction.mockClear();
    await fireEvent.click(opened);
    expect(opened.getAttribute("aria-pressed")).toBe("false");
    await waitFor(() =>
      expect(instance.dispatchAction).toHaveBeenCalledWith({
        type: "downplay",
        seriesIndex: 0,
      }),
    );
  });

  it("never hides a series the way the built-in legend did", async () => {
    const { container } = mount();
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    await fireEvent.click(container.querySelector(".chart-legend__item"));
    await waitFor(() => expect(instance.dispatchAction).toHaveBeenCalled());
    for (const [action] of instance.dispatchAction.mock.calls) {
      expect(action.type).not.toContain("legend");
    }
  });

  it("highlights on hover and on keyboard focus", async () => {
    const { container } = mount();
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const [, closed] = container.querySelectorAll(".chart-legend__item");

    instance.dispatchAction.mockClear();
    await fireEvent.mouseEnter(closed);
    await waitFor(() =>
      expect(instance.dispatchAction).toHaveBeenCalledWith({
        type: "highlight",
        seriesIndex: 1,
      }),
    );

    // Leaving drops the highlight, so the chart is not left emphasised.
    instance.dispatchAction.mockClear();
    await fireEvent.mouseLeave(closed);
    await waitFor(() =>
      expect(instance.dispatchAction).toHaveBeenCalledWith({
        type: "downplay",
        seriesIndex: 1,
      }),
    );

    instance.dispatchAction.mockClear();
    await fireEvent.focus(closed);
    await waitFor(() =>
      expect(instance.dispatchAction).toHaveBeenCalledWith({
        type: "highlight",
        seriesIndex: 1,
      }),
    );
  });

  it("lists pie slices rather than the one pie series", async () => {
    const { container } = mount({
      option: {
        series: [
          {
            type: "pie",
            name: "Share",
            radius: ["40%", "65%"],
            data: [
              { name: "Done", value: 3 },
              { name: "Open", value: 2 },
            ],
          },
        ],
      },
    });
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    expect(
      [...container.querySelectorAll(".chart-legend__name")].map(
        (node) => node.textContent,
      ),
    ).toEqual(["Done", "Open"]);
  });

  it("gives ECharts a tooltip formatter that leads with the hovered series", async () => {
    mount();
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const { formatter } = instance.setOption.mock.calls[0][0].tooltip;
    expect(typeof formatter).toBe("function");

    // Hovering the second series makes it the lead, over the larger first one.
    handlers.mouseover({ seriesIndex: 1 });
    const element = formatter([
      {
        seriesIndex: 0,
        seriesName: "Opened",
        value: 6,
        color: "#111111",
        axisValueLabel: "Tue",
        dataIndex: 1,
      },
      {
        seriesIndex: 1,
        seriesName: "Closed",
        value: 3,
        color: "#222222",
        axisValueLabel: "Tue",
        dataIndex: 1,
      },
    ]);

    const lead = element.querySelector(".report-tooltip__lead");
    expect(lead.textContent).toContain("Closed");
    expect(lead.textContent).toContain("3");
    expect(lead.textContent).toContain("33% of total");
    // Previous point in the same series is 1, so +2 on a base of 1.
    expect(element.querySelector(".report-tooltip__delta").textContent).toBe(
      "+2 (+200%) vs previous",
    );
    expect(
      element.querySelector(".report-tooltip__rest").textContent,
    ).toContain("Opened");
  });

  it("builds tooltip text as text, never as markup", async () => {
    mount({
      option: {
        xAxis: { type: "category", data: ["Mon"] },
        yAxis: { type: "value" },
        series: [
          { type: "line", name: "<img src=x onerror=alert(1)>", data: [1] },
          { type: "line", name: "Closed", data: [2] },
        ],
      },
    });
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const { formatter } = instance.setOption.mock.calls[0][0].tooltip;
    const element = formatter([
      {
        seriesIndex: 0,
        seriesName: "<img src=x onerror=alert(1)>",
        value: 1,
        color: "#111111",
        axisValueLabel: "Mon",
        dataIndex: 0,
      },
    ]);
    // The name survives as readable text and creates no element.
    expect(element.querySelector(".report-tooltip__name").textContent).toBe(
      "<img src=x onerror=alert(1)>",
    );
    expect(element.querySelector("img")).toBeNull();
  });

  it("drops the hovered series when the pointer leaves the chart", async () => {
    mount();
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const { formatter } = instance.setOption.mock.calls[0][0].tooltip;
    handlers.mouseover({ seriesIndex: 1 });
    handlers.globalout();
    const element = formatter([
      {
        seriesIndex: 0,
        seriesName: "Opened",
        value: 6,
        color: "#111111",
        axisValueLabel: "Tue",
        dataIndex: 1,
      },
      {
        seriesIndex: 1,
        seriesName: "Closed",
        value: 3,
        color: "#222222",
        axisValueLabel: "Tue",
        dataIndex: 1,
      },
    ]);
    // With no hover, the largest value at that point leads.
    expect(
      element.querySelector(".report-tooltip__lead").textContent,
    ).toContain("Opened");
  });

  it("rewrites the tooltip already on screen when the pointer moves series", async () => {
    const { container } = mount();
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const { formatter } = instance.setOption.mock.calls[0][0].tooltip;
    const points = [
      {
        seriesIndex: 0,
        seriesName: "Opened",
        value: 6,
        color: "#111111",
        axisValueLabel: "Tue",
        dataIndex: 1,
      },
      {
        seriesIndex: 1,
        seriesName: "Closed",
        value: 3,
        color: "#222222",
        axisValueLabel: "Tue",
        dataIndex: 1,
      },
    ];

    handlers.mouseover({ seriesIndex: 0, dataIndex: 1 });
    const rendered = formatter(points);
    expect(
      rendered.querySelector(".report-tooltip__lead").textContent,
    ).toContain("Opened");

    // ECharts puts the formatted element in its own container and will not
    // format again for the same axis position, so the fix rewrites what is
    // already on screen. Stand that element up where the component looks.
    container.querySelector(".chart-surface").append(rendered);

    handlers.mouseover({ seriesIndex: 1, dataIndex: 1 });
    expect(
      rendered.querySelector(".report-tooltip__lead").textContent,
    ).toContain("Closed");
  });

  it("leaves the tooltip alone when the hovered series has not changed", async () => {
    const { container } = mount();
    await waitFor(() => expect(instance.setOption).toHaveBeenCalledOnce());
    const { formatter } = instance.setOption.mock.calls[0][0].tooltip;
    handlers.mouseover({ seriesIndex: 1, dataIndex: 1 });
    const rendered = formatter([
      {
        seriesIndex: 1,
        seriesName: "Closed",
        value: 3,
        color: "#222222",
        axisValueLabel: "Tue",
        dataIndex: 1,
      },
    ]);
    container.querySelector(".chart-surface").append(rendered);
    const before = rendered.innerHTML;
    handlers.mouseover({ seriesIndex: 1, dataIndex: 1 });
    expect(rendered.innerHTML).toBe(before);
  });
});
