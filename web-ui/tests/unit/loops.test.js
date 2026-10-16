import { describe, expect, it } from "vitest";
import {
  compareLoops,
  goalGap,
  goalGeometry,
  loopTotals,
  normalizeLoop,
  normalizeLoopStats,
  normalizeRunReceipt,
  runText,
  withUnit,
} from "../../src/lib/loops.js";

const RAW = {
  health: "stalled",
  health_reason: "No effect in 9d (budget 7d)",
  every_seconds: 86400,
  progress_budget_seconds: 604800,
  last_run_at: "2026-10-15T08:00:00Z",
  last_effect_at: "2026-10-06T08:00:00Z",
  next_run_due_at: "2026-10-16T08:00:00Z",
  recent_runs: [
    { at: "2026-10-15T08:00:00Z", outcome: "measured", decision: "rejected" },
    { at: "2026-10-14T08:00:00Z", outcome: "no_op" },
    { at: "2026-10-13T08:00:00Z", outcome: "bogus" },
  ],
  runs_7d: { acted: 0, measured: 4, no_op: 2, infra_error: 1 },
  goal: {
    metric: "avo.score",
    direction: "up",
    target: 0.9,
    unit: "",
    latest: 0.8,
    best: 0.84,
    points: [
      { at: "2026-10-03T00:00:00Z", value: 0.7 },
      { at: "2026-10-01T00:00:00Z", value: 0.6 },
      { at: "2026-10-05T00:00:00Z", value: 0.84, decision: "kept" },
      { at: "2026-10-07T00:00:00Z", value: "NaN" },
      { at: "2026-10-09T00:00:00Z", value: 0.8, decision: "reverted" },
    ],
  },
};

describe("loops model", () => {
  it("normalizes the computed loop, dropping unreadable runs and points", () => {
    const loop = normalizeLoop(RAW);
    expect(loop.condition).toBe("stalled");
    expect(loop.recentRuns.map((run) => run.outcome)).toEqual([
      "no_op",
      "measured",
    ]);
    expect(loop.goal.points.map((p) => p.value)).toEqual([0.6, 0.7, 0.84, 0.8]);
    expect(normalizeLoop(null)).toBeNull();
    expect(normalizeLoop({ health: "weird" }).condition).toBe("unknown");
  });

  it("orders needs-attention loops first, then by last effect", () => {
    const row = (condition, lastEffectAt = "") => ({
      loop: { condition, lastEffectAt },
    });
    const rows = [
      row("healthy", "2026-10-01T00:00:00Z"),
      row("unknown"),
      row("healthy", "2026-10-10T00:00:00Z"),
      row("failing"),
      row("stalled"),
    ].sort(compareLoops);
    expect(rows.map((r) => r.loop.condition)).toEqual([
      "failing",
      "stalled",
      "healthy",
      "healthy",
      "unknown",
    ]);
    expect(rows[2].loop.lastEffectAt).toBe("2026-10-10T00:00:00Z");
  });

  it("totals runs and attention across loops", () => {
    const totals = loopTotals([
      normalizeLoop(RAW),
      normalizeLoop({ health: "healthy", runs_7d: { acted: 3 } }),
    ]);
    expect(totals).toMatchObject({
      loops: 2,
      effects7d: 3,
      needsAttention: 1,
      runs7d: { acted: 3, measured: 4, no_op: 2, infra_error: 1 },
    });
    expect(
      normalizeLoopStats({ loops: 2, effects_7d: 5 }).effectsPrev7d,
    ).toBeNull();
  });

  it("draws best-so-far in the goal's direction", () => {
    const up = normalizeLoop(RAW).goal;
    const geo = goalGeometry(up, { width: 100, height: 100, pad: 0 });
    // The best line never goes back down after 0.84.
    const bestYs = geo.best.split(" ").map((p) => Number(p.split(",")[1]));
    expect(Math.min(...bestYs)).toBeCloseTo(bestYs.at(-1));
    expect(geo.markers.map((m) => m.decision)).toEqual(["kept", "reverted"]);
    expect(geo.target).not.toBeNull();

    const down = { ...up, direction: "down", target: null };
    const downGeo = goalGeometry(down, { width: 100, height: 100, pad: 0 });
    const downYs = downGeo.best.split(" ").map((p) => Number(p.split(",")[1]));
    // Lower is better: the best value is the first (0.6), so the line stays at its y.
    expect(new Set(downYs).size).toBe(1);
    expect(goalGeometry({ ...up, points: [] })).toBeNull();
  });

  it("handles a flat or single-point series without dividing by zero", () => {
    const one = goalGeometry({
      direction: "up",
      target: null,
      points: [{ at: "2026-10-01T00:00:00Z", value: 5 }],
    });
    expect(one.raw).not.toMatch(/NaN/);
  });

  it("measures the gap to target in the goal's direction", () => {
    expect(goalGap({ direction: "up", target: 0.9, best: 0.84 })).toBeCloseTo(
      0.06,
    );
    expect(goalGap({ direction: "down", target: 10, best: 8 })).toBe(-2);
    expect(goalGap({ direction: "up", target: null, best: 1 })).toBeNull();
  });

  it("reads a run receipt and names it", () => {
    const run = normalizeRunReceipt({
      at: "2026-10-15T08:00:00Z",
      outcome: "acted",
      summary: "Kept candidate c7",
      experiment: {
        candidate: "c7",
        decision: "kept",
        score_before: 0.8,
        score_after: 0.84,
      },
      metrics: { "avo.score": 0.84, bad: "x" },
      effect_refs: ["https://github.com/o/r/pull/1", ""],
    });
    expect(run.decision).toBe("kept");
    expect(run.metrics).toEqual([{ name: "avo.score", value: 0.84 }]);
    expect(run.effectRefs).toEqual(["https://github.com/o/r/pull/1"]);
    expect(runText(run)).toBe("acted, kept");
    expect(withUnit("84", "%")).toBe("84%");
    expect(withUnit("3", "GB")).toBe("3 GB");
  });
});
