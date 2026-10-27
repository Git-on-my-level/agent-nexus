import { describe, expect, it } from "vitest";

import {
  formatDuration,
  formatTrendValue,
  panelMax,
  sparklinePath,
  trendChartLabel,
  trendDelta,
  trendRange,
  trendTakeaway,
} from "../../src/lib/watchTrends.js";

const panel = (extra) => ({
  id: "x",
  title: "X",
  unit: "count",
  direction: "up_good",
  value: 10,
  previous: 5,
  truncated: false,
  series: [],
  rows: [],
  ...extra,
});

describe("trendRange", () => {
  it("reads 7d, 30d and 90d and defaults to 30", () => {
    expect(trendRange(new URLSearchParams("range=7d"))).toBe(7);
    expect(trendRange(new URLSearchParams("range=90d"))).toBe(90);
    expect(trendRange(new URLSearchParams("range=1y"))).toBe(30);
    expect(trendRange(new URLSearchParams(""))).toBe(30);
  });
});

describe("formatting", () => {
  it("words durations without jargon", () => {
    expect(formatDuration(45)).toBe("45 s");
    expect(formatDuration(30 * 60)).toBe("30 min");
    expect(formatDuration(3.5 * 3600)).toBe("3.5 h");
    expect(formatDuration(3 * 86400)).toBe("3 days");
  });
  it("shows unknown as a dash, never zero", () => {
    expect(formatTrendValue("count", null)).toBe("—");
    expect(formatTrendValue("percent", 66.6)).toBe("67%");
  });
});

describe("trendDelta", () => {
  it("colours by which direction is better", () => {
    expect(trendDelta(panel({ value: 10, previous: 5 }))).toEqual({
      label: "▲ 2×",
      tone: "good",
    });
    expect(
      trendDelta(panel({ direction: "down_good", value: 12, previous: 10 })),
    ).toEqual({ label: "▲ 20%", tone: "bad" });
    expect(
      trendDelta(panel({ unit: "percent", value: 60, previous: 75 })).label,
    ).toBe("▼ 15 pts");
    expect(trendDelta(panel({ value: 10, previous: 10 })).tone).toBe("flat");
    expect(trendDelta(panel({ previous: null }))).toBeNull();
  });
});

describe("trendTakeaway", () => {
  it("says answer time went up, with the factor", () => {
    expect(
      trendTakeaway(
        panel({
          id: "answer_time",
          unit: "seconds",
          direction: "down_good",
          value: 2.3 * 3600,
          previous: 3600,
        }),
        30,
      ),
    ).toBe("Answer time up 2.3× vs the previous 30 days, now 2.3 h.");
  });
  it("says when asks are piling up", () => {
    expect(
      trendTakeaway(panel({ id: "asks_flow", value: 4, raised: 9 }), 7),
    ).toBe("Asks are piling up: 9 raised, 4 answered.");
  });
  it("counts initiatives past their promise", () => {
    expect(
      trendTakeaway(
        panel({
          id: "past_promise",
          unit: "percent",
          value: 75,
          previous: 50,
          past: 24,
          total: 32,
        }),
        30,
      ),
    ).toBe("24 of 32 initiatives are past their promise, up 25 pts.");
  });
  it("names who needed the most help", () => {
    expect(
      trendTakeaway(
        panel({
          id: "ask_sources",
          value: null,
          rows: [
            {
              principal: { id: "a", display: "builder" },
              value: 9,
              previous: 3,
            },
          ],
        }),
        30,
      ),
    ).toBe("builder needed the most help: 9 asks, up from 3.");
  });
  it("says a fall to zero in words, never as a factor", () => {
    expect(
      trendTakeaway(panel({ id: "loop_runs", value: 0, previous: 12 }), 30),
    ).toBe("Runs down to none vs the previous 30 days.");
    for (const id of [
      "asks_flow",
      "answer_time",
      "work_flow",
      "loop_runs",
      "x",
    ])
      expect(
        trendTakeaway(
          panel({ id, value: 0, previous: 5, raised: 0, started: 0 }),
          7,
        ),
      ).not.toMatch(/Infinity|NaN/);
  });

  it("never calls a truncated, empty ranking no activity", () => {
    for (const id of ["ask_sources", "agent_runs"]) {
      const said = trendTakeaway(
        panel({ id, value: null, previous: null, truncated: true, rows: [] }),
        30,
      );
      expect(said).not.toMatch(/^No /);
      expect(said).toMatch(/Busy period/);
    }
    expect(
      trendTakeaway(panel({ id: "ask_sources", value: null, rows: [] }), 30),
    ).toBe("No asks raised in this period.");
  });

  it("admits a capped read instead of guessing", () => {
    expect(
      trendTakeaway(
        panel({ id: "loop_runs", value: null, truncated: true }),
        30,
      ),
    ).toMatch(/only the most recent records/);
    expect(trendTakeaway(panel({ id: "loop_runs", value: null }), 30)).toBe(
      "Not enough history yet.",
    );
  });
});

describe("sparklinePath", () => {
  it("breaks the line at unknown buckets", () => {
    expect(
      sparklinePath([1, null, 2, 4], { width: 30, height: 10, max: 4 }),
    ).toBe("M0 7M20 5L30 1");
    expect(sparklinePath([null, null])).toBe("");
  });
  it("shares one scale per panel, 100 for a single share", () => {
    expect(
      panelMax({
        unit: "count",
        series: [{ points: [1, 7] }, { points: [3, null] }],
      }),
    ).toBe(7);
    expect(panelMax({ unit: "percent", series: [{ points: [20] }] })).toBe(100);
    expect(
      panelMax({ unit: "percent", series: [{ points: [2] }, { points: [3] }] }),
    ).toBe(3);
  });
});

describe("trendChartLabel", () => {
  it("words each series in its own unit, not the panel summary's", () => {
    const label = trendChartLabel({
      title: "Promises kept",
      unit: "percent",
      series: [
        { key: "kept", label: "Kept", unit: "count", points: [2, null, 1] },
        { key: "missed", label: "Missed", unit: "count", points: [0, 3, 1] },
      ],
    });
    expect(label).toBe("Promises kept: Kept 2, unknown, 1; Missed 0, 3, 1");
    expect(label).not.toMatch(/%/);
  });
  it("keeps percentages for a share series", () => {
    expect(
      trendChartLabel({
        title: "Past promise",
        unit: "percent",
        series: [{ label: "Past", unit: "percent", points: [50] }],
      }),
    ).toBe("Past promise: Past 50%");
  });
});
