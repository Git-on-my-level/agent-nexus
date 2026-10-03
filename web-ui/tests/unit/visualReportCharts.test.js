import { describe, expect, it } from "vitest";
import { init } from "echarts/core";
import "../../src/lib/components/reports/reportChartRenderer.js";
import {
  buildReportChartOption,
  reportChartRows,
  validateReportChart,
} from "../../src/lib/visualReportCharts.js";
import {
  portfolioReviewReport,
  swarmObservatoryReport,
} from "../../src/lib/fixtures/expressiveReportExamples.js";

const chart = (type = "line") => ({
  caption: "Observed values; missing observations are gaps.",
  option: {
    xAxis: { type: "category", name: "Day", data: ["Mon", "Tue", "Wed"] },
    yAxis: { type: "value", name: "Change" },
    series: [{ type, name: "Observed", data: [-5, null, 8] }],
  },
});
const pie = () => ({
  option: {
    series: [
      {
        type: "pie",
        name: "Share",
        radius: ["40%", "65%"],
        data: [
          { name: "Complete", value: 8 },
          { name: "Pending", value: 2 },
        ],
      },
    ],
  },
});
const fromExample = (type) =>
  structuredClone(
    [...portfolioReviewReport.panels, ...swarmObservatoryReport.panels].find(
      (panel) =>
        panel.type === "chart" && panel.data.option.series[0].type === type,
    ).data,
  );
const expectInvalid = (data) => {
  expect(validateReportChart(data).length).toBeGreaterThan(0);
  expect(buildReportChartOption(data)).toBeNull();
};

describe("bounded ECharts report vocabulary", () => {
  it("accepts every expressive example and returns new, deterministic owned options", () => {
    for (const panel of [
      ...portfolioReviewReport.panels,
      ...swarmObservatoryReport.panels,
    ]) {
      if (panel.type !== "chart") continue;
      const before = structuredClone(panel.data);
      expect(validateReportChart(panel.data)).toEqual([]);
      const option = buildReportChartOption(panel.data);
      expect(option).toEqual(buildReportChartOption(panel.data));
      expect(option).not.toBe(panel.data.option);
      expect(option.tooltip.renderMode).toBe("richText");
      expect(option.animation).toBe(false);
      expect(panel.data).toEqual(before);
      expect(reportChartRows(panel.data).rows.length).toBeGreaterThan(0);
    }
  });

  it("preserves signed values, zeros and null gaps in lines, areas and bars", () => {
    for (const type of ["line", "bar", "scatter"]) {
      const data = chart(type);
      if (type === "line") data.option.series[0].areaStyle = {};
      const option = buildReportChartOption(data);
      expect(option.series[0].data).toEqual([-5, null, 8]);
      expect(option.yAxis[0].scale).toBe(false);
      if (type === "line") {
        expect(option.series[0].connectNulls).toBe(false);
        expect(option.series[0].areaStyle.opacity).toBeGreaterThan(0);
      }
      expect(reportChartRows(data).rows[1][3]).toBe("No observation");
    }
    const data = chart();
    data.option.series[0].data = [0, null, 0];
    expect(buildReportChartOption(data).series[0].data).toEqual([0, null, 0]);
  });

  it("supports horizontal stacked bars and mixed plots with a second value axis", () => {
    const data = chart("bar");
    [data.option.xAxis, data.option.yAxis] = [
      data.option.yAxis,
      data.option.xAxis,
    ];
    data.option.series[0].stack = "Work";
    data.option.series.push({
      type: "bar",
      name: "Planned",
      data: [2, 3, 4],
      stack: "Work",
    });
    expect(validateReportChart(data)).toEqual([]);
    expect(reportChartRows(data).rows[0]).toEqual([
      "Observed",
      "1",
      "-5",
      "Mon",
      "",
    ]);
    const mixed = chart("bar");
    mixed.option.yAxis = [
      mixed.option.yAxis,
      { type: "value", position: "right", name: "Rate" },
    ];
    mixed.option.series.push({
      type: "line",
      name: "Rate",
      data: [0.2, 0.4, 0.8],
      yAxisIndex: 1,
    });
    expect(validateReportChart(mixed)).toEqual([]);
    expect(buildReportChartOption(mixed).series[1].yAxisIndex).toBe(1);
  });

  it("supports modern epoch-millisecond time coordinates and rejects invalid dates", () => {
    const data = chart();
    data.option.xAxis = { type: "time", name: "Observed at" };
    data.option.series[0].data = [
      [Date.parse("2026-10-03T12:00:00Z"), -2],
      [Date.parse("2026-10-04T12:00:00Z"), null],
    ];
    expect(validateReportChart(data)).toEqual([]);
    expect(buildReportChartOption(data).useUTC).toBe(true);
    data.option.series[0].data[0][0] = 8.64e15 + 1;
    expectInvalid(data);
  });

  it("treats numeric category labels literally instead of category indices", () => {
    const data = chart();
    data.option.xAxis.data = [10, 20];
    data.option.series[0].data = [
      [10, 2],
      [20, 3],
    ];
    const option = buildReportChartOption(data);
    expect(option.xAxis[0].data).toEqual(["10", "20"]);
    expect(option.series[0].data).toEqual([
      ["10", 2],
      ["20", 3],
    ]);
  });

  it("supports donuts and rose charts without implying shares for zero totals", () => {
    const data = pie();
    data.option.series[0].roseType = "area";
    expect(validateReportChart(data)).toEqual([]);
    expect(buildReportChartOption(data).series[0].radius).toEqual([
      "40%",
      "65%",
    ]);
    expect(buildReportChartOption(data).series[0].stillShowZeroSum).toBe(false);
    data.option.series[0].data[0].value = -1;
    expectInvalid(data);
  });

  it("rejects extreme tiny values before pie and sankey scales can overflow", () => {
    const donut = pie();
    donut.option.series[0].data[0].value = 1e-320;
    expectInvalid(donut);
    const flow = {
      option: {
        series: [
          {
            type: "sankey",
            name: "Flow",
            data: [{ name: "A" }, { name: "B" }],
            links: [{ source: "A", target: "B", value: 1e-320 }],
          },
        ],
      },
    };
    expectInvalid(flow);
    donut.option.series[0].data[0].value = 1e-100;
    flow.option.series[0].links[0].value = 1e-100;
    expect(validateReportChart(donut)).toEqual([]);
    expect(validateReportChart(flow)).toEqual([]);
    const small = chart();
    small.option.series[0].data = [-1e-12, 0, 1e-12];
    expect(validateReportChart(small)).toEqual([]);
  });

  it("keeps missing heatmap cells out of color ranges and labels", () => {
    const data = {
      option: {
        xAxis: { type: "category", data: ["Mon", "Tue"] },
        yAxis: { type: "category", data: ["A"] },
        series: [
          {
            type: "heatmap",
            name: "Load",
            data: [
              [0, 0, -2],
              [1, 0, null],
            ],
          },
        ],
      },
    };
    const option = buildReportChartOption(data);
    expect(option.series[0].data).toEqual([[0, 0, -2]]);
    expect(option.visualMap.min).toBe(-2);
    expect(reportChartRows(data).rows[1][4]).toBe("No observation");
    data.option.series[0].data.push([1, 0, 3]);
    expectInvalid(data);
  });

  it("rejects cycles and dangling references in sankey data", () => {
    const data = {
      option: {
        series: [
          {
            type: "sankey",
            name: "Flow",
            data: [{ name: "A" }, { name: "B" }],
            links: [{ source: "A", target: "B", value: 4 }],
          },
        ],
      },
    };
    expect(validateReportChart(data)).toEqual([]);
    data.option.series[0].links.push({ source: "B", target: "A", value: 1 });
    expectInvalid(data);
    data.option.series[0].links = [
      { source: "A", target: "Missing", value: 1 },
    ];
    expectInvalid(data);
  });

  it("rejects zero-total and no-link sankey charts before they can produce invalid geometry", () => {
    const data = {
      option: {
        series: [
          {
            type: "sankey",
            name: "Flow",
            data: [{ name: "A" }, { name: "B" }],
            links: [{ source: "A", target: "B", value: 0 }],
          },
        ],
      },
    };
    expectInvalid(data);
    expect(validateReportChart(data).join(" ")).toContain("positive flow");
    data.option.series[0].links = [];
    expectInvalid(data);
  });

  it("rejects ambiguous scalar values on two category axes", () => {
    const data = chart("bar");
    data.option.yAxis = { type: "category", data: ["C", "D"] };
    data.option.series[0].data = [1, 2];
    expectInvalid(data);
    data.option.series[0].type = "scatter";
    data.option.series[0].data = [
      ["Mon", "C"],
      ["Tue", "D"],
    ];
    expect(validateReportChart(data)).toEqual([]);
  });

  it("bounds fixed graph geometry and tree depth", () => {
    const graph = fromExample("graph");
    graph.option.series[0].layout = "none";
    expectInvalid(graph);
    graph.option.series[0].data.forEach((node, index) => {
      node.x = index * 10;
      node.y = 20;
    });
    expect(validateReportChart(graph)).toEqual([]);
    const tree = {
      option: {
        series: [
          { type: "treemap", name: "Tree", data: [{ name: "Leaf", value: 1 }] },
        ],
      },
    };
    for (let i = 0; i < 5; i++)
      tree.option.series[0].data = [
        { name: "Branch", children: tree.option.series[0].data },
      ];
    expectInvalid(tree);
  });

  it("derives tree branch values from children so a zero parent cannot hide positive evidence", () => {
    const data = {
      option: {
        series: [
          {
            type: "treemap",
            name: "Effort",
            data: [
              {
                name: "Group",
                value: 0,
                children: [{ name: "Work", value: 1 }],
              },
            ],
          },
        ],
      },
    };
    expectInvalid(data);
    delete data.option.series[0].data[0].value;
    expect(validateReportChart(data)).toEqual([]);
    expect(reportChartRows(data).rows[0][4]).toBe("Sum of children");
  });

  it.each([NaN, Infinity, -Infinity, 1e13, "12", {}, () => 4])(
    "rejects nonnumeric or unbounded values: %s",
    (value) => {
      const data = chart();
      data.option.series[0].data[0] = value;
      expectInvalid(data);
    },
  );

  it.each([
    (d) => {
      d.option.tooltip = { formatter: "<img src=x onerror=alert(1)>" };
    },
    (d) => {
      d.option.tooltip = {
        renderMode: "html",
        extraCssText: "background:url(https://evil.test)",
      };
    },
    (d) => {
      d.option.title = { text: "Title", link: "javascript:alert(1)" };
    },
    (d) => {
      d.option.toolbox = { feature: { dataView: { show: true } } };
    },
    (d) => {
      d.option.graphic = [
        { type: "image", style: { image: "https://evil.test" } },
      ];
    },
    (d) => {
      d.option.dataset = {
        source: [],
        transform: { type: "filter", config: { reg: "(a+)+$" } },
      };
    },
    (d) => {
      d.option.series[0].type = "custom";
      d.option.series[0].renderItem = "alert(1)";
    },
    (d) => {
      d.option.series[0].symbol = "image://https://evil.test";
    },
    (d) => {
      d.option.series[0].label = { formatter: () => "unsafe" };
    },
    (d) => {
      d.option.series[0].areaStyle = { color: { image: "https://evil.test" } };
    },
    (d) => {
      d.option.xAxis.axisLabel = { formatter: "{value}" };
    },
    (d) => {
      d.option.series[0].connectNulls = true;
    },
    (d) => {
      d.option.series[0].data[0] = { value: 1, link: "https://evil.test" };
    },
  ])(
    "rejects unsafe options rather than silently forwarding them",
    (change) => {
      const data = chart();
      change(data);
      expectInvalid(data);
    },
  );

  it("rejects prototype keys and unbounded payloads without echoing them in diagnostics", () => {
    const data = chart();
    data.option = JSON.parse(
      JSON.stringify(data.option).replace(
        '"series":',
        '"__proto__":{"polluted":true},"series":',
      ),
    );
    expectInvalid(data);
    expect(validateReportChart(data).join(" ")).not.toContain("__proto__");
    const oversized = chart();
    oversized.option.series[0].data = Array(201).fill(1);
    expectInvalid(oversized);
    const many = chart();
    many.option.series = Array.from({ length: 13 }, (_, i) => ({
      type: "bar",
      name: String(i),
      data: [1],
    }));
    expectInvalid(many);
  });

  it("copies data deeply enough that ECharts cannot mutate the source", () => {
    for (const data of [
      pie(),
      fromExample("graph"),
      fromExample("treemap"),
      fromExample("scatter"),
    ]) {
      const snapshot = structuredClone(data);
      const option = buildReportChartOption(data);
      if (Array.isArray(option.series[0].data[0]))
        option.series[0].data[0][0] = 777;
      else option.series[0].data[0].name = "Changed";
      expect(data).toEqual(snapshot);
    }
  });

  it("uses owned theme tokens, rejecting CSS or URL values", () => {
    const option = buildReportChartOption(chart(), {
      fg: "#112233",
      muted: "#445566",
      panel: "url(https://evil.test)",
    });
    expect(option.textStyle.color).toBe("#112233");
    expect(option.yAxis[0].axisLabel.color).toBe("#445566");
    expect(option.tooltip.backgroundColor).toBe("#161922");
  });
});

describe("real SVG rendering", () => {
  it.each(["horizontal", "vertical"])(
    "keeps dense %s sankey nodes finite and inside the plot",
    (orient) => {
      const sources = Array.from({ length: 119 }, (_, i) => ({
        name: `Source ${i}`,
      }));
      const data = {
        option: {
          series: [
            {
              type: "sankey",
              name: "Dense flow",
              orient,
              data: [...sources, { name: "Sink" }],
              links: sources.map(({ name }) => ({
                source: name,
                target: "Sink",
                value: 1,
              })),
            },
          ],
        },
      };
      const instance = init(null, null, {
        renderer: "svg",
        ssr: true,
        width: 300,
        height: 280,
      });
      try {
        instance.setOption(buildReportChartOption(data));
        const svg = instance.renderToSVGString();
        expect(svg).not.toMatch(/NaN|Infinity/);
        const nodes = instance.getModel().getSeriesByIndex(0).getData();
        for (let i = 0; i < nodes.count(); i++) {
          const { x, y, dx, dy } = nodes.getItemLayout(i);
          for (const value of [x, y, dx, dy]) {
            expect(Number.isFinite(value)).toBe(true);
            expect(value).toBeGreaterThanOrEqual(-1e-8);
          }
          expect(x + dx).toBeLessThanOrEqual(300 * 0.97 - 112 + 1e-8);
          expect(y + dy).toBeLessThanOrEqual(280 * 0.72 + 1e-8);
        }
      } finally {
        instance.dispose();
      }
    },
  );

  const examples = [
    ...portfolioReviewReport.panels,
    ...swarmObservatoryReport.panels,
  ]
    .filter((panel) => panel.type === "chart")
    .map((panel) => [panel.id, panel.data]);
  examples.push(["line with gaps", chart()], ["donut", pie()]);
  it.each(examples)(
    "renders %s using only SVG and finite geometry",
    (_, data) => {
      const instance = init(null, null, {
        renderer: "svg",
        ssr: true,
        width: 640,
        height: 360,
      });
      try {
        instance.setOption(buildReportChartOption(data));
        const svg = instance.renderToSVGString();
        expect(svg).toContain("<svg");
        expect(svg).not.toMatch(/NaN|Infinity|<image|<script|foreignObject/);
        expect(svg).toMatch(/<path|<rect|<circle/);
      } finally {
        instance.dispose();
      }
    },
  );

  it("renders hostile labels as escaped SVG text, never links or markup", () => {
    const data = pie();
    data.option.series[0].data[0].name = "<script>alert(1)</script>";
    const instance = init(null, null, {
      renderer: "svg",
      ssr: true,
      width: 640,
      height: 360,
    });
    try {
      instance.setOption(buildReportChartOption(data));
      const svg = instance.renderToSVGString();
      expect(svg).not.toContain("<script>");
      expect(svg).not.toMatch(/href=|<image|foreignObject/);
      expect(svg).toContain("&lt;script&gt;");
    } finally {
      instance.dispose();
    }
  });
});
