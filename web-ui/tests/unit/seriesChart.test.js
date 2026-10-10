import { describe, expect, it } from "vitest";

import {
  partialBucketStart,
  seriesBucketMs,
  seriesChartData,
  seriesStreamLabel,
} from "../../src/lib/seriesChart.js";
import { validateReportChart } from "../../src/lib/visualReportCharts.js";

const DAY = 86_400_000;
const START = Date.parse("2026-10-05T00:00:00Z");
/** Inside the last bucket: the one still filling. */
const NOW = START + 5 * DAY + 3 * 3_600_000;

/** A chart exactly as core materializes a bound panel: lines on a time axis. */
const liveChart = (streams) => ({
  option: {
    xAxis: { type: "time" },
    yAxis: { type: "value", name: "PRs" },
    series: streams.map(({ name, values }) => ({
      name,
      type: "line",
      data: values.map((value, index) => [START + index * DAY, value]),
    })),
  },
});

const panel = ({ streams, fallback } = {}) => ({
  id: "prs",
  type: "chart",
  title: "PRs merged per day",
  source: { series: "prs-merged" },
  data: liveChart(
    streams ?? [
      { name: "prs-merged repo=oss", values: [1, 4, 7, 5, 6, 2] },
      { name: "prs-merged repo=saas", values: [0, 2, 3, 2, 1, 0] },
    ],
  ),
  seriesFallback: false,
  ...(fallback ? { fallback } : {}),
});

/** The authored snapshot that declares what the panel is. */
const authored = (series, extra = {}) => ({
  as_of: "2026-10-01T00:00:00Z",
  data: {
    option: {
      xAxis: { type: "category", data: ["Mon", "Tue"] },
      yAxis: { type: "value" },
      series,
    },
    ...extra,
  },
});

describe("a stream's human name", () => {
  it("keeps the label values and drops the series name every stream shares", () => {
    expect(seriesStreamLabel("prs-merged repo=oss", "prs-merged")).toBe("oss");
    expect(
      seriesStreamLabel("prs-merged repo=oss env=prod", "prs-merged"),
    ).toBe("oss · prod");
  });

  it("leaves a name it does not recognise alone", () => {
    expect(seriesStreamLabel("prs-merged", "prs-merged")).toBe("prs-merged");
    expect(seriesStreamLabel("something else", "prs-merged")).toBe(
      "something else",
    );
    expect(seriesStreamLabel("", "prs-merged")).toBe("");
  });
});

describe("the bucket still filling", () => {
  it("reads the step from the gaps core actually returned", () => {
    expect(seriesBucketMs(panel().data.option.series)).toBe(DAY);
  });

  it("is the last bucket when now falls inside it", () => {
    expect(partialBucketStart(panel().data.option.series, NOW)).toBe(
      START + 5 * DAY,
    );
  });

  it("is nothing once the publisher has gone quiet", () => {
    // A day and a half after the last bucket closed: no bucket is filling.
    expect(
      partialBucketStart(panel().data.option.series, START + 7 * DAY),
    ).toBeNull();
  });
});

describe("a bound chart with no authored declaration", () => {
  const data = seriesChartData(panel(), { now: NOW });

  it("renames the legend without touching the values", () => {
    expect(data.option.series.map((entry) => entry.name)).toEqual([
      "oss",
      "saas",
    ]);
    expect(data.option.series[0].data).toEqual(
      panel().data.option.series[0].data,
    );
  });

  it("marks the partial bucket once, on the first series", () => {
    expect(data.option.series[0].markLine.data).toEqual([
      { name: "Partial", xAxis: START + 5 * DAY },
    ]);
    expect(data.option.series[1].markLine).toBeUndefined();
  });

  it("stays a chart this build will render", () => {
    expect(validateReportChart(data)).toEqual([]);
  });

  it("keeps core's names when two labels would collide", () => {
    const collide = panel({
      streams: [
        { name: "prs-merged repo=oss", values: [1, 2] },
        { name: "prs-merged other=oss", values: [3, 4] },
      ],
    });
    const out = seriesChartData(collide, { now: NOW });
    expect(out.option.series.map((entry) => entry.name)).toEqual([
      "prs-merged repo=oss",
      "prs-merged other=oss",
    ]);
    expect(validateReportChart(out)).toEqual([]);
  });
});

describe("a bound chart whose author declared bars", () => {
  const withBars = panel({
    fallback: authored(
      [
        { type: "bar", name: "OSS", stack: "repos", data: [1, 2] },
        { type: "bar", name: "SaaS", stack: "repos", data: [1, 2] },
      ],
      { palette: "forest", caption: "Merged per day" },
    ),
  });
  const data = seriesChartData(withBars, { now: NOW });

  it("renders the declared type, stack and names", () => {
    expect(data.option.series.map((entry) => entry.type)).toEqual([
      "bar",
      "bar",
    ]);
    expect(data.option.series.map((entry) => entry.stack)).toEqual([
      "repos",
      "repos",
    ]);
    expect(data.option.series.map((entry) => entry.name)).toEqual([
      "OSS",
      "SaaS",
    ]);
    expect(data.palette).toBe("forest");
  });

  it("leaves the snapshot's caption on the snapshot", () => {
    // A caption is a claim about numbers. The snapshot it was written for is
    // not on screen, so it must not sit under this week's chart.
    expect(data.caption).toBeUndefined();
  });

  it("rebins onto the buckets core returned, losing no value", () => {
    expect(data.option.xAxis[0].type).toBe("category");
    expect(data.option.xAxis[0].data).toHaveLength(6);
    expect(data.option.series[0].data).toEqual([1, 4, 7, 5, 6, 2]);
    expect(data.option.series[1].data).toEqual([0, 2, 3, 2, 1, 0]);
  });

  it("says in the axis which bucket is still filling", () => {
    // A category axis takes no reference line, so the label carries it.
    expect(data.option.xAxis[0].data.at(-1)).toMatch(/\(so far\)$/);
    expect(data.option.xAxis[0].data.at(-2)).not.toMatch(/so far/);
  });

  it("stays a chart this build will render", () => {
    expect(validateReportChart(data)).toEqual([]);
  });

  it("fills a bucket a stream never reported with null, not zero", () => {
    const sparse = panel({
      streams: [{ name: "prs-merged repo=oss", values: [1, 2, 3] }],
      fallback: authored([{ type: "bar", name: "OSS", data: [1, 2] }]),
    });
    sparse.data.option.series.push({
      name: "prs-merged repo=saas",
      type: "line",
      // Starts a day late: its first bucket has no point at all.
      data: [
        [START + DAY, 5],
        [START + 2 * DAY, 6],
      ],
    });
    const out = seriesChartData(sparse, { now: START + 2 * DAY + 3600_000 });
    expect(out.option.series[1].data).toEqual([null, 5, 6]);
  });
});

describe("matching the author's names to the live streams", () => {
  /*
   * The live list is not stable. Core drops a stream with no points in the
   * window and orders label sets by their JSON, so a quiet week or one new
   * label shifts every later stream. Matching by index would put "OSS" on
   * SaaS's numbers, with nothing on screen to show it had happened.
   */
  const withNames = (streams) =>
    seriesChartData(
      panel({
        streams,
        fallback: authored([
          { type: "bar", name: "OSS", data: [1] },
          { type: "bar", name: "SaaS", data: [1] },
        ]),
      }),
      { now: NOW },
    ).option.series.map((entry) => [entry.name, entry.data]);

  it("follows the labels when a quiet stream drops out", () => {
    expect(
      withNames([{ name: "prs-merged repo=saas", values: [1, 2, 1] }]),
    ).toEqual([["SaaS", [1, 2, 1]]]);
  });

  it("follows the labels when a new one sorts in front", () => {
    const out = withNames([
      { name: "prs-merged repo=internal", values: [9, 9, 9] },
      { name: "prs-merged repo=oss", values: [4, 6, 5] },
      { name: "prs-merged repo=saas", values: [1, 2, 1] },
    ]);
    expect(out.map(([name]) => name)).toEqual(["internal", "OSS", "SaaS"]);
    // The unmatched stream keeps its own values and its own label; only the
    // two the author named take the author's names.
    expect(out[0][1]).toEqual([9, 9, 9]);
    expect(out[1][1]).toEqual([4, 6, 5]);
  });

  it("matches a declared name to the label value it names", () => {
    // `team=core` is "Core". Identity, not order: the same answer comes out
    // whichever way round the author listed them.
    const out = (declared) =>
      seriesChartData(
        panel({
          streams: [
            { name: "prs-merged team=core", values: [1, 2] },
            { name: "prs-merged team=apps", values: [3, 4] },
          ],
          fallback: authored(declared),
        }),
        { now: NOW },
      ).option.series.map((entry) => [entry.name, entry.data]);
    const inOrder = out([
      { type: "bar", name: "Core", data: [1] },
      { type: "bar", name: "Apps", data: [1] },
    ]);
    const reversed = out([
      { type: "bar", name: "Apps", data: [1] },
      { type: "bar", name: "Core", data: [1] },
    ]);
    expect(inOrder.map(([name]) => name)).toEqual(["Core", "Apps"]);
    expect(reversed).toEqual(inOrder);
  });

  it("never pairs a name with a stream by position", () => {
    /*
     * The swap this exists to prevent. Live streams sort `repo=oss` then
     * `repo=saas`; the author wrote "SaaS PRs" then "OSS PRs". Neither name
     * is a label value, so nothing is verified — and pairing them by index
     * would have drawn OSS's numbers under "SaaS PRs" with nothing on
     * screen to show it. Each stream keeps the label it actually has.
     */
    const out = seriesChartData(
      panel({
        streams: [
          { name: "prs-merged repo=oss", values: [7, 7, 7] },
          { name: "prs-merged repo=saas", values: [1, 1, 1] },
        ],
        fallback: authored([
          { type: "bar", name: "SaaS PRs", data: [1] },
          { type: "bar", name: "OSS PRs", data: [1] },
        ]),
      }),
      { now: NOW },
    );
    expect(out.option.series.map((entry) => entry.name)).toEqual([
      "oss",
      "saas",
    ]);
    /*
     * The bars still arrive. A name claims *which* stream this is and needs
     * identity; a type claims only that the panel is a bar chart, which the
     * author did say and which cannot mislabel anything.
     */
    expect(out.option.series.map((entry) => entry.type)).toEqual([
      "bar",
      "bar",
    ]);
    // Bars rebin onto a category axis, so the values arrive as scalars —
    // still OSS's own numbers, under OSS's own label.
    expect(out.option.series[0].data).toEqual([7, 7, 7]);
    expect(validateReportChart(out)).toEqual([]);
  });

  it("never lets a word inside one value identify another stream", () => {
    /*
     * Core joins label pairs with spaces and a *value* may contain spaces of
     * its own, so splitting on every space minted `saas` out of
     * `owner=dave saas` — another stream's whole identity. A lone stream
     * belonging to Dave would then have been drawn as "SaaS", bars, stack
     * and all, which is the misattribution this rule exists to prevent.
     */
    const out = seriesChartData(
      panel({
        streams: [{ name: "prs-merged owner=dave saas", values: [1, 2] }],
        fallback: authored([
          { type: "bar", name: "SaaS", stack: "repos", data: [1] },
        ]),
      }),
      { now: NOW },
    );
    expect(out.option.series[0].name).toBe("dave saas");
    // The panel is still the stacked bar chart its author declared; what it
    // must not do is call Dave's stream SaaS.
    expect(out.option.series[0].type).toBe("bar");
    expect(validateReportChart(out)).toEqual([]);
  });

  it("reads a multi-word value whole, so a real name still matches", () => {
    // `team=repo` is unambiguously "repo": the neighbour's `repo=my repo`
    // holds one value, "my repo", and lends no word of it to anyone.
    const out = seriesChartData(
      panel({
        streams: [
          { name: "prs-merged repo=my repo", values: [1, 2] },
          { name: "prs-merged team=repo", values: [3, 4] },
        ],
        fallback: authored([{ type: "bar", name: "repo", data: [1] }]),
      }),
      { now: NOW },
    );
    expect(out.option.series.map((entry) => entry.name)).toEqual([
      "my repo",
      "repo",
    ]);
    expect(validateReportChart(out)).toEqual([]);
  });

  it("refuses a name two streams both answer to", () => {
    // `repo=oss` and `env=oss` both identify as "oss": neither is verified,
    // so both keep core's own name rather than one of them taking "OSS".
    const out = seriesChartData(
      panel({
        streams: [
          { name: "prs-merged repo=oss", values: [1, 2] },
          { name: "prs-merged env=oss", values: [3, 4] },
        ],
        fallback: authored([{ type: "bar", name: "OSS", data: [1] }]),
      }),
      { now: NOW },
    );
    expect(out.option.series.map((entry) => entry.name)).toEqual([
      "prs-merged repo=oss",
      "prs-merged env=oss",
    ]);
  });

  it("refuses a name a second, self-ambiguous stream also answers to", () => {
    /*
     * `repo=oss team=saas` answers to both declarations, so it takes
     * neither — but it is still a claimant, and a declaration two streams
     * answer to identifies neither of them. Counting claims only among
     * streams that had already resolved let `env=oss` walk off with "OSS".
     */
    const out = seriesChartData(
      panel({
        streams: [
          { name: "prs-merged env=oss", values: [1, 2] },
          { name: "prs-merged repo=oss team=saas", values: [3, 4] },
        ],
        fallback: authored([
          { type: "bar", name: "OSS", data: [1] },
          { type: "bar", name: "SaaS", data: [1] },
        ]),
      }),
      { now: NOW },
    );
    expect(out.option.series.map((entry) => entry.name)).toEqual([
      "oss",
      "oss · saas",
    ]);
  });

  it("draws the panel's declared shape even where it cannot name a stream", () => {
    /*
     * The commonest authored panel: one declared series, named something the
     * labels do not contain. Refusing the name is right; refusing the bars
     * with it meant every such panel silently became a line chart.
     */
    const out = seriesChartData(
      panel({
        streams: [{ name: "prs-merged repo=oss", values: [1, 2] }],
        fallback: authored([
          { type: "bar", name: "Pull requests", barWidth: 20, data: [1] },
        ]),
      }),
      { now: NOW },
    );
    expect(out.option.series[0].name).toBe("oss");
    expect(out.option.series[0].type).toBe("bar");
    expect(out.option.series[0].barWidth).toBe(20);
    expect(validateReportChart(out)).toEqual([]);
  });

  it("takes no shape at all when the declarations disagree on one", () => {
    // Two different drawings and no verified identity: there is no single
    // thing the author asked for, so core's own shape stands.
    const out = seriesChartData(
      panel({
        streams: [
          { name: "prs-merged repo=oss", values: [1, 2] },
          { name: "prs-merged repo=saas", values: [3, 4] },
        ],
        fallback: authored([
          { type: "bar", name: "Merged", data: [1] },
          { type: "scatter", name: "Reviewed", data: [1] },
        ]),
      }),
      { now: NOW },
    );
    expect(out.option.series.map((entry) => entry.type)).toEqual([
      "line",
      "line",
    ]);
  });

  it("keeps a legend hidden by the author only once every stream is named", () => {
    const hidden = (streams) => {
      const p = panel({
        streams,
        fallback: authored([{ type: "bar", name: "OSS", data: [1] }]),
      });
      p.fallback.data.option.legend = { show: false };
      return seriesChartData(p, { now: NOW }).option.legend;
    };
    expect(hidden([{ name: "prs-merged repo=oss", values: [1] }])).toEqual({
      show: false,
    });
    // Two live streams, one declared: hiding the legend here would leave the
    // second one unlabelled, which is the thing this file exists to fix.
    expect(
      hidden([
        { name: "prs-merged repo=oss", values: [1] },
        { name: "prs-merged repo=saas", values: [2] },
      ]),
    ).toBeUndefined();
  });
});

describe("what it refuses to restyle", () => {
  it("leaves the authored snapshot alone when that is what is showing", () => {
    const fallingBack = {
      ...panel(),
      seriesFallback: true,
      data: { option: { series: [{ type: "bar", name: "OSS", data: [1] }] } },
    };
    expect(seriesChartData(fallingBack, { now: NOW })).toBe(fallingBack.data);
  });

  it("leaves a panel core could not materialize alone", () => {
    const empty = { ...panel(), data: {} };
    expect(seriesChartData(empty, { now: NOW })).toEqual({});
  });

  it("drops a stack it cannot honour rather than failing validation", () => {
    /*
     * Two streams binned half a day apart: their union is 220 buckets, more
     * categories than one chart may carry. Converting anyway would produce a
     * chart this build refuses to render at all, so the panel keeps its time
     * axis and loses only the stack that axis cannot hold.
     */
    const offset = panel({
      streams: [{ name: "prs-merged repo=oss", values: [1] }],
      fallback: authored([
        { type: "line", name: "OSS", stack: "a", data: [1] },
      ]),
    });
    const run = (base) =>
      Array.from({ length: 110 }, (_, index) => [
        base + index * DAY,
        index % 7,
      ]);
    offset.data.option.series = [
      { name: "prs-merged repo=oss", type: "line", data: run(START) },
      {
        name: "prs-merged repo=saas",
        type: "line",
        data: run(START + DAY / 2),
      },
    ];
    const out = seriesChartData(offset, { now: START });
    expect(out.option.xAxis.type).toBe("time");
    expect(out.option.series[0].stack).toBeUndefined();
    expect(validateReportChart(out)).toEqual([]);
  });

  it("keeps the time axis when rebinning would blow the point budget", () => {
    /*
     * Eight streams, each reporting 150 of the same 200 buckets: 1,200 points
     * as core sent them, which is exactly what a chart may carry. Filling
     * every bucket for every stream would make it 1,600 and the chart would
     * not render at all — and an unrendered chart is worse than a line.
     */
    const busy = panel({
      streams: [{ name: "prs-merged repo=oss", values: [1] }],
      fallback: authored([{ type: "bar", name: "OSS", data: [1] }]),
    });
    busy.data.option.series = Array.from({ length: 8 }, (_, stream) => ({
      name: `prs-merged repo=r${stream}`,
      type: "line",
      data: Array.from({ length: 200 }, (_, index) => index)
        .filter((index) => (index + stream) % 4 !== 0)
        .map((index) => [START + index * DAY, index % 5]),
    }));
    const out = seriesChartData(busy, { now: START });
    expect(out.option.xAxis.type).toBe("time");
    expect(validateReportChart(out)).toEqual([]);
  });

  it("keeps the time axis rather than folding two points onto one bucket", () => {
    // Core bins, so a stream never reports one instant twice — but folding
    // it would keep the last of the two and lose a point invisibly.
    const twice = panel({
      streams: [{ name: "prs-merged repo=oss", values: [1, 2, 3] }],
      fallback: authored([{ type: "bar", name: "OSS", data: [1] }]),
    });
    twice.data.option.series[0].data.push([START + DAY, 9]);
    const out = seriesChartData(twice, { now: START });
    expect(out.option.xAxis.type).toBe("time");
    expect(out.option.series[0].data).toHaveLength(4);
    expect(validateReportChart(out)).toEqual([]);
  });

  it("marks the partial bucket when it cannot convert to categories", () => {
    // A declared-bar panel that keeps its time axis still has today's
    // unfinished bucket; without the line, that last point reads as a crash.
    const twice = panel({
      streams: [{ name: "prs-merged repo=oss", values: [1, 2, 3] }],
      fallback: authored([{ type: "bar", name: "OSS", data: [1] }]),
    });
    twice.data.option.series[0].data.push([START + DAY, 9]);
    const out = seriesChartData(twice, { now: START + 2 * DAY + 3_600_000 });
    expect(out.option.xAxis.type).toBe("time");
    expect(out.option.series[0].markLine.data).toEqual([
      { name: "Partial", xAxis: START + 2 * DAY },
    ]);
    expect(validateReportChart(out)).toEqual([]);
  });

  it("keeps the axis a live series was plotted against", () => {
    /*
     * Core sends one y-axis today, but a core that sends two would have a
     * percentage series re-scaled onto a count axis if the index were
     * dropped — a silently wrong chart, which is worse than an unstyled one.
     */
    const paired = panel({
      streams: [{ name: "prs-merged repo=oss", values: [1, 2] }],
      fallback: authored([{ type: "line", name: "OSS", data: [1] }]),
    });
    paired.data.option.yAxis = [
      { type: "value", name: "PRs" },
      { type: "value", name: "%" },
    ];
    paired.data.option.series.push({
      name: "prs-merged repo=share",
      type: "line",
      yAxisIndex: 1,
      smooth: true,
      data: [
        [START, 40],
        [START + DAY, 60],
      ],
    });
    const out = seriesChartData(paired, { now: NOW });
    expect(out.option.series[1].yAxisIndex).toBe(1);
    expect(out.option.series[1].smooth).toBe(true);
    expect(validateReportChart(out)).toEqual([]);
  });

  it("labels daily buckets without repeating the year on every one", () => {
    const data = seriesChartData(
      panel({
        fallback: authored([{ type: "bar", name: "OSS", data: [1] }]),
      }),
      { now: NOW },
    );
    expect(data.option.xAxis[0].data[0]).toMatch(/^[A-Z][a-z]{2} \d+$/);
  });
});
