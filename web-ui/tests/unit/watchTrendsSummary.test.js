import { describe, expect, it } from "vitest";

import {
  areaStatus,
  bucketLabel,
  knownTail,
  panelReading,
  trendsVerdict,
} from "../../src/lib/watchTrends.js";

const flow = (extra = {}) => ({
  id: "asks_flow",
  unit: "count",
  direction: "up_good",
  value: 41,
  previous: 52,
  raised: 58,
  truncated: false,
  series: [
    { key: "opened", label: "Raised", points: [1, 2, 3] },
    { key: "answered", label: "Answered", points: [1, 1, 2] },
  ],
  rows: [],
  ...extra,
});

describe("panelReading", () => {
  it("shows the value and its change when the period is known", () => {
    const r = panelReading(flow());
    expect(r.text).toBe("41");
    expect(r.delta.tone).toBe("bad");
    expect(r.partial).toBe("");
  });

  it("totals the buckets that were read instead of showing a dash", () => {
    const r = panelReading(
      flow({
        value: null,
        previous: null,
        truncated: true,
        series: [{ key: "answered", label: "Answered", points: [null, 4, 5] }],
      }),
    );
    expect(r).toEqual({ text: "9", delta: null, partial: "last 2 days" });
    const weeks = panelReading(
      flow({
        value: null,
        series: [{ key: "answered", label: "Answered", points: [null, 4] }],
      }),
      "week",
    );
    expect(weeks.partial).toBe("last 1 week");
  });

  it("keeps a dash for shares and medians with no known period", () => {
    expect(
      panelReading({
        id: "answer_time",
        unit: "seconds",
        value: null,
        series: [{ key: "median", points: [null, 60] }],
      }).text,
    ).toBe("—");
  });
});

it("knownTail counts the most recent known buckets", () => {
  expect(knownTail([null, 1, null, 2, 3])).toBe(2);
  expect(knownTail([])).toBe(0);
});

it("bucketLabel names a day or a week in UTC", () => {
  expect(bucketLabel("2026-10-08T00:00:00Z")).toBe("Oct 8");
  expect(bucketLabel("2026-10-08T00:00:00Z", "week")).toBe("Week of Oct 8");
  expect(bucketLabel("nope")).toBe("");
});

describe("areaStatus", () => {
  it("warns when open asks grow and says when there is no history", () => {
    expect(
      areaStatus("decisions", [
        { id: "ask_backlog", value: 32, previous: 9 },
        flow(),
      ]),
    ).toEqual({ tone: "warn", label: "Backlog growing" });
    expect(areaStatus("decisions", [flow({ raised: 10, value: 12 })])).toEqual({
      tone: "ok",
      label: "Keeping up",
    });
    expect(areaStatus("decisions", [])).toEqual({
      tone: "none",
      label: "Not enough history",
    });
  });

  it("flags late loops and failures", () => {
    expect(areaStatus("loops", [{ id: "loop_health", value: 82 }])).toEqual({
      tone: "warn",
      label: "82% on time",
    });
    expect(
      areaStatus("loops", [
        { id: "loop_health", value: 100 },
        { id: "loop_runs", value: 9, failed: 2 },
      ]),
    ).toEqual({ tone: "warn", label: "2 failed runs" });
    expect(
      areaStatus("loops", [{ id: "loop_runs", value: 9, failed: 0 }]),
    ).toEqual({ tone: "ok", label: "Healthy" });
  });

  it("calls initiatives slipping when more are past their promise", () => {
    expect(
      areaStatus("initiatives", [
        {
          id: "past_promise",
          unit: "percent",
          direction: "down_good",
          value: 75,
          previous: 50,
        },
      ]).tone,
    ).toBe("warn");
  });
});

describe("trendsVerdict", () => {
  it("leads with the backlog and lists what to look at", () => {
    const v = trendsVerdict(
      {
        decisions: [
          flow(),
          { id: "ask_backlog", unit: "count", value: 32, previous: 9 },
          {
            id: "answer_time",
            unit: "seconds",
            direction: "down_good",
            value: 3600,
            previous: 7200,
          },
        ],
        initiatives: [
          { id: "promises", unit: "percent", value: 81 },
          {
            id: "past_promise",
            unit: "percent",
            value: 15,
            past: 3,
            total: 20,
          },
        ],
        loops: [{ id: "loop_health", unit: "percent", value: 82 }],
      },
      30,
    );
    expect(v.lead).toBe("Open asks rose from 9 to 32 over the last 30 days.");
    expect(v.rest).toBe(
      "Answers got faster (60 min typical), 81% of promises were kept, 3 of 20 initiatives are past their promise and 82% of loops are reporting on time.",
    );
    expect(v.actions.map((a) => a.key)).toEqual([
      "decide",
      "initiatives",
      "loops",
    ]);
  });

  it("says nothing without numbers", () => {
    expect(trendsVerdict({}, 7)).toEqual({ lead: "", rest: "", actions: [] });
  });

  it("falls back to asks raised vs answered over a week", () => {
    expect(trendsVerdict({ decisions: [flow()] }, 7).lead).toBe(
      "Asks are outpacing answers: 58 raised, 41 answered over the last week.",
    );
  });
});
