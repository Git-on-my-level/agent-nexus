import { describe, expect, it } from "vitest";

import {
  formatChartDelta,
  formatChartValue,
  previousPointValues,
  reportLegendModel,
  reportTooltipModel,
  TOOLTIP_SERIES_LIMIT,
} from "../../src/lib/reportChartInteraction.js";
import { REPORT_CHART_PALETTES } from "../../src/lib/visualReportCharts.js";

const DARK_BG = "#0b0d12";

const point = (seriesIndex, seriesName, value, extra = {}) => ({
  seriesIndex,
  seriesName,
  value,
  color: `#00000${seriesIndex}`,
  axisValueLabel: "Tue",
  dataIndex: 1,
  ...extra,
});

describe("formatChartValue", () => {
  it("separates thousands on whole numbers", () => {
    expect(formatChartValue(12400)).toBe("12,400");
  });

  it("keeps precision proportional to magnitude", () => {
    expect(formatChartValue(0.125)).toBe("0.13");
    expect(formatChartValue(4.25)).toBe("4.3");
    expect(formatChartValue(412.5)).toBe("413");
  });

  it("says so when there is no value", () => {
    expect(formatChartValue(null)).toBe("No value");
    expect(formatChartValue(Number.NaN)).toBe("No value");
  });
});

describe("formatChartDelta", () => {
  it("reports a rise with its percentage", () => {
    expect(formatChartDelta(12, 10)).toEqual({
      direction: "up",
      text: "+2 (+20%) vs previous",
    });
  });

  it("reports a fall with a minus sign", () => {
    const delta = formatChartDelta(8, 10);
    expect(delta.direction).toBe("down");
    expect(delta.text).toBe("−2 (−20%) vs previous");
  });

  it("calls an unchanged value no change", () => {
    expect(formatChartDelta(10, 10)).toEqual({
      direction: "flat",
      text: "No change",
    });
  });

  it("omits the percentage when the baseline is zero", () => {
    expect(formatChartDelta(5, 0).text).toBe("+5 vs previous");
  });

  it("returns nothing when there is no previous value", () => {
    expect(formatChartDelta(5, undefined)).toBeNull();
    expect(formatChartDelta(5, null)).toBeNull();
  });
});

describe("reportLegendModel", () => {
  const seriesChart = (count) => ({
    option: {
      series: Array.from({ length: count }, (_, index) => ({
        type: "line",
        name: `Series ${index + 1}`,
        data: [1],
      })),
    },
  });

  it("lists one item per series, coloured from the theme ramp", () => {
    const model = reportLegendModel(seriesChart(2), DARK_BG);
    expect(model.show).toBe(true);
    expect(model.items.map((item) => item.name)).toEqual([
      "Series 1",
      "Series 2",
    ]);
    expect(model.items[0].color).toBe(REPORT_CHART_PALETTES.ocean.dark[0]);
    expect(model.items[1].color).toBe(REPORT_CHART_PALETTES.ocean.dark[1]);
  });

  it("hides a single-series legend that would only repeat the title", () => {
    expect(reportLegendModel(seriesChart(1), DARK_BG).show).toBe(false);
  });

  it("shows a single-series legend when the author asks", () => {
    const data = seriesChart(1);
    data.option.legend = { show: true };
    expect(reportLegendModel(data, DARK_BG).show).toBe(true);
  });

  it("lets an author hide a multi-series legend", () => {
    const data = seriesChart(3);
    data.option.legend = { show: false };
    expect(reportLegendModel(data, DARK_BG).show).toBe(false);
  });

  it("lists pie slices rather than the single pie series", () => {
    const model = reportLegendModel(
      {
        option: {
          series: [
            {
              type: "pie",
              data: [
                { name: "Done", value: 3 },
                { name: "Open", value: 2 },
              ],
            },
          ],
        },
      },
      DARK_BG,
    );
    expect(model.items.map((item) => item.name)).toEqual(["Done", "Open"]);
    expect(model.items[0].dataName).toBe("Done");
  });

  it("lists graph categories when a graph declares them", () => {
    const model = reportLegendModel(
      {
        option: {
          series: [
            {
              type: "graph",
              categories: [{ name: "Core" }, { name: "UI" }],
              data: [],
            },
          ],
        },
      },
      DARK_BG,
    );
    expect(model.items.map((item) => item.name)).toEqual(["Core", "UI"]);
  });

  it("follows the surface for its colours", () => {
    const light = reportLegendModel(seriesChart(2), "#ffffff");
    expect(light.items[0].color).toBe(REPORT_CHART_PALETTES.ocean.light[0]);
  });

  it("cycles colours when there are more series than ramp entries", () => {
    const model = reportLegendModel(seriesChart(8), DARK_BG);
    expect(model.items[6].color).toBe(REPORT_CHART_PALETTES.ocean.dark[0]);
  });

  it("returns nothing for a chart with no series", () => {
    expect(reportLegendModel({ option: { series: [] } }, DARK_BG)).toEqual({
      show: false,
      items: [],
    });
    expect(reportLegendModel(null, DARK_BG).show).toBe(false);
  });
});

describe("reportTooltipModel", () => {
  it("leads with the hovered series, not the first one", () => {
    const model = reportTooltipModel({
      points: [point(0, "Opened", 4), point(1, "Closed", 6)],
      hoveredSeriesIndex: 1,
    });
    expect(model.lead.name).toBe("Closed");
    expect(model.rest.map((row) => row.name)).toEqual(["Opened"]);
  });

  it("carries the value, the share of total and the delta on the lead", () => {
    const model = reportTooltipModel({
      points: [point(0, "Opened", 30), point(1, "Closed", 70)],
      hoveredSeriesIndex: 0,
      previous: { 0: 24 },
    });
    expect(model.lead.valueText).toBe("30");
    expect(model.lead.share).toBeCloseTo(30, 5);
    expect(model.lead.delta.text).toBe("+6 (+25%) vs previous");
    expect(model.axisLabel).toBe("Tue");
  });

  it("falls back to the largest value when the cursor series is unknown", () => {
    const model = reportTooltipModel({
      points: [
        point(0, "Opened", 4),
        point(1, "Closed", 9),
        point(2, "Held", 2),
      ],
    });
    expect(model.lead.name).toBe("Closed");
  });

  it("omits the share when values have mixed signs", () => {
    const model = reportTooltipModel({
      points: [point(0, "Gain", 10), point(1, "Loss", -4)],
      hoveredSeriesIndex: 0,
    });
    expect(model.lead.share).toBeNull();
    expect(model.rest[0].share).toBeNull();
  });

  it("omits the share when the total is zero", () => {
    const model = reportTooltipModel({
      points: [point(0, "A", 0), point(1, "B", 0)],
      hoveredSeriesIndex: 0,
    });
    expect(model.lead.share).toBeNull();
  });

  it("omits the delta when no previous point exists", () => {
    const model = reportTooltipModel({
      points: [point(0, "Opened", 4)],
      hoveredSeriesIndex: 0,
    });
    expect(model.lead.delta).toBeNull();
  });

  it("collapses the tail into a count", () => {
    const points = Array.from({ length: 9 }, (_, index) =>
      point(index, `Series ${index}`, 10 - index),
    );
    const model = reportTooltipModel({ points, hoveredSeriesIndex: 0 });
    expect(model.rest).toHaveLength(TOOLTIP_SERIES_LIMIT);
    expect(model.more).toBe(8 - TOOLTIP_SERIES_LIMIT);
  });

  it("reports no overflow when every series fits", () => {
    const model = reportTooltipModel({
      points: [point(0, "A", 2), point(1, "B", 1)],
      hoveredSeriesIndex: 0,
    });
    expect(model.more).toBe(0);
  });

  it("handles a gap in a series without claiming a value", () => {
    const model = reportTooltipModel({
      points: [point(0, "Opened", null), point(1, "Closed", 3)],
      hoveredSeriesIndex: 0,
    });
    expect(model.lead.valueText).toBe("No value");
    expect(model.lead.share).toBeNull();
  });

  it("reads the measured value out of a scatter pair", () => {
    const model = reportTooltipModel({
      points: [point(0, "Latency", [3, 42])],
      hoveredSeriesIndex: 0,
    });
    expect(model.lead.valueText).toBe("42");
  });

  it("returns nothing when there are no points", () => {
    expect(reportTooltipModel({ points: [] })).toBeNull();
    expect(reportTooltipModel()).toBeNull();
  });
});

describe("previousPointValues", () => {
  const series = [
    { type: "line", name: "Opened", data: [1, 5, 9] },
    { type: "line", name: "Closed", data: [2, 6, null] },
  ];

  it("reads the point before the hovered one in each series", () => {
    expect(previousPointValues(series, 2)).toEqual({ 0: 5, 1: 6 });
  });

  it("gives nothing at the first point, where there is no previous period", () => {
    expect(previousPointValues(series, 0)).toEqual({});
  });

  it("skips a series whose previous point is a gap", () => {
    expect(previousPointValues([{ data: [null, 4] }], 1)).toEqual({});
  });

  it("tolerates a missing series list", () => {
    expect(previousPointValues(undefined, 2)).toEqual({});
    expect(previousPointValues(series, Number.NaN)).toEqual({});
  });
});
