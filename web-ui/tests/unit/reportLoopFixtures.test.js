import { describe, expect, it } from "vitest";
import loops from "../fixtures/report-templates/loops.json";
import hillClimb from "../fixtures/report-templates/hill-climb.json";

const outcomes = ["acted", "measured", "no_op", "infra_error"];
const zeroCounts = () => Object.fromEntries(outcomes.map((key) => [key, 0]));
const now = Date.parse(loops.observations[0].observed_at);
const ageDays = (at) => (now - Date.parse(at)) / 86_400_000;
const incumbent = (point) =>
  point.decision == null || point.decision === "kept";
const effect = (run) => run.outcome === "acted" || run.decision === "kept";

describe("loop report fixtures", () => {
  it("keeps run counts, health, goal values and overview totals coherent", () => {
    const { stats, loops: rows } = loops.observations[0].data;
    const totals = {
      loops: rows.length,
      runs_7d: zeroCounts(),
      effects_7d: 0,
      effects_prev_7d: 0,
      needs_attention: 0,
      improved_7d: 0,
    };
    for (const { loop } of rows) {
      const counts = zeroCounts();
      let effects = 0;
      // Goal points preserve the complete fixture history beyond the recent-run cap.
      const runs = new Map(loop.recent_runs.map((run) => [run.at, run]));
      for (const point of loop.goal?.points ?? []) {
        if (!runs.has(point.at))
          runs.set(point.at, {
            ...point,
            outcome: point.decision === "kept" ? "acted" : "measured",
          });
      }
      for (const run of runs.values()) {
        expect(ageDays(run.at)).toBeGreaterThanOrEqual(0);
        if (ageDays(run.at) <= 7) counts[run.outcome]++;
        if (effect(run)) {
          if (ageDays(run.at) <= 7) {
            totals.effects_7d++;
            effects++;
          } else if (ageDays(run.at) <= 14) totals.effects_prev_7d++;
        }
      }
      expect(loop.runs_7d).toEqual(counts);
      expect(loop.effects_7d).toBe(effects);
      for (const key of outcomes) totals.runs_7d[key] += counts[key];
      expect(loop.last_run_at).toBe(loop.recent_runs[0]?.at ?? null);
      if (loop.health === "failing")
        expect(loop.recent_runs.slice(0, 2).map((run) => run.outcome)).toEqual([
          "infra_error",
          "infra_error",
        ]);
      if (loop.health === "overdue")
        expect(ageDays(loop.last_run_at) * 86400).toBeGreaterThan(
          loop.every_seconds * 2,
        );
      if (loop.health === "stalled") {
        expect(ageDays(loop.last_run_at) * 86400).toBeLessThanOrEqual(
          loop.every_seconds * 2,
        );
        expect(ageDays(loop.last_effect_at) * 86400).toBeGreaterThan(
          loop.progress_budget_seconds,
        );
      }
      if (loop.health === "healthy")
        expect(ageDays(loop.last_effect_at) * 86400).toBeLessThanOrEqual(
          loop.progress_budget_seconds,
        );
      if (loop.health_reason === "no runs") {
        expect(loop.recent_runs).toEqual([]);
        expect(loop.last_run_at).toBeNull();
        expect(counts).toEqual(zeroCounts());
      }
      if (["failing", "overdue", "stalled"].includes(loop.health))
        totals.needs_attention++;
      if (loop.goal) {
        const points = loop.goal.points.filter(incumbent);
        expect(loop.goal.latest).toBe(points.at(-1).value);
        expect(loop.goal.best).toBe(
          Math.max(...points.map((point) => point.value)),
        );
        expect(
          Math.max(
            ...loop.goal.points
              .filter((point) => !incumbent(point))
              .map((point) => point.value),
          ),
        ).toBeGreaterThan(loop.goal.best);
        const before = points.filter((point) => ageDays(point.at) > 7);
        const after = points.filter((point) => ageDays(point.at) <= 7);
        if (
          after.length &&
          Math.max(...after.map((point) => point.value)) >
            Math.max(...before.map((point) => point.value))
        )
          totals.improved_7d++;
      }
    }
    expect(stats).toEqual(totals);
  });

  it("uses the same optimizer state and complete receipt history in hill-climb detail", () => {
    const optimizer = loops.observations[0].data.loops.find(
      (row) => row.title === "Optimizer",
    );
    const detail = hillClimb.observations[0].data;
    expect(detail.loop).toEqual(optimizer.loop);
    expect(
      detail.runs.slice(0, 12).map((run) => ({
        at: run.at,
        outcome: run.outcome,
        decision: run.experiment?.decision ?? null,
      })),
    ).toEqual(detail.loop.recent_runs);
    expect(
      detail.runs
        .map((run) => ({
          at: run.at,
          value: run.metrics["quality.score"],
          decision: run.experiment.decision,
        }))
        .reverse(),
    ).toEqual(detail.loop.goal.points);
  });
});
