import { describe, expect, it } from "vitest";

import {
  collapseRuns,
  goalGeometry,
  loopPanelRows,
  normalizeLoop,
  shortCandidate,
} from "../../src/lib/loops.js";
import {
  collectionFromLoops,
  dominantQuietState,
  untagged,
} from "../../src/lib/collection.js";

const DAY = 86_400_000;
const NOW = Date.parse("2026-10-23T09:00:00Z");

describe("collapseRuns", () => {
  it("folds consecutive identical runs and keeps the span", () => {
    const run = (at, extra = {}) => ({
      at,
      outcome: "no_op",
      decision: null,
      summary: "no_op battery",
      effectRefs: [],
      experiment: null,
      ...extra,
    });
    const groups = collapseRuns([
      run("2026-10-16T03:00:00Z"),
      run("2026-10-15T03:00:00Z"),
      run("2026-10-14T03:00:00Z"),
      run("2026-10-13T03:00:00Z", { outcome: "measured" }),
      run("2026-10-12T03:00:00Z", {
        experiment: { candidate: "abc" },
      }),
      run("2026-10-11T03:00:00Z", {
        experiment: { candidate: "abc" },
      }),
    ]);
    expect(groups.map((g) => [g.outcome, g.count])).toEqual([
      ["no_op", 3],
      ["measured", 1],
      ["no_op", 1],
      ["no_op", 1],
    ]);
    expect(groups[0].oldestAt).toBe("2026-10-14T03:00:00Z");
  });
});

describe("shortCandidate", () => {
  it("shortens long hashes only", () => {
    expect(shortCandidate("72e77635b54f5c41939c3dc59339ae43")).toBe("72e7763");
    expect(shortCandidate("prompt-v2")).toBe("prompt-v2");
  });
});

describe("goalGeometry with a window", () => {
  const goal = {
    metric: "f1",
    direction: "up",
    target: null,
    best: 1,
    points: [
      { at: "2026-10-16T21:18:20Z", value: 1, decision: null },
      { at: "2026-10-16T21:18:52Z", value: 1, decision: null },
    ],
  };
  it("places a 32-second burst inside a 30-day axis, and draws the silence", () => {
    const geo = goalGeometry(goal, {
      width: 300,
      height: 60,
      pad: 0,
      window: { start: NOW - 30 * DAY, end: NOW },
    });
    const xs = geo.raw.split(" ").map((pair) => Number(pair.split(",")[0]));
    expect(xs[1] - xs[0]).toBeLessThan(1);
    expect(xs[0]).toBeGreaterThan(200);
    expect(geo.silence).not.toBeNull();
    expect(geo.silence.x2).toBe(300);
  });
  it("keeps first-to-last without a window", () => {
    const geo = goalGeometry(goal, { width: 300, height: 60, pad: 0 });
    expect(geo.silence).toBeNull();
  });
});

describe("loop panel rows", () => {
  const loop = (health, lastRunAt) =>
    normalizeLoop({ health, last_run_at: lastRunAt, runs_7d: {} });
  it("says once when nearly every loop stopped reporting", () => {
    const rows = loopPanelRows(
      ["a", "b", "c", "d", "e"].map((id, i) => ({
        ref: `card:${id}`,
        title: id,
        loop: loop(i === 4 ? "healthy" : "overdue", "2026-10-16T15:45:00Z"),
      })),
    );
    const collection = untagged(collectionFromLoops(rows, { now: NOW }));
    expect(dominantQuietState(collection)).toEqual({
      state: "silent",
      count: 4,
      total: 5,
    });
    const tagged = collection.groups[0].rows.flatMap((r) =>
      [r.stateField, r.time?.field].filter(Boolean),
    );
    expect(tagged).toEqual([]);
  });
});
