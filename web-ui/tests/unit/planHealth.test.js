import { describe, expect, it } from "vitest";

import {
  ATTENTION_ORDER,
  attentionRank,
  humanizeStepId,
  nextStepModel,
  planHealthModel,
} from "../../src/lib/planHealth.js";

describe("attentionRank", () => {
  it("ranks past promise first, done last", () => {
    expect(ATTENTION_ORDER).toEqual([
      "past_promise",
      "no_promise",
      "moving",
      "done",
    ]);
    expect(attentionRank("past_promise")).toBeLessThan(
      attentionRank("no_promise"),
    );
    expect(attentionRank("no_promise")).toBeLessThan(attentionRank("moving"));
    expect(attentionRank("moving")).toBeLessThan(attentionRank("done"));
  });

  it("sorts an unknown or removed state with the tail", () => {
    for (const state of ["something-new", "", "stale", "no_plan"]) {
      expect(attentionRank(state)).toBeGreaterThan(attentionRank("done"));
    }
  });
});

describe("planHealthModel", () => {
  it("reads a past promise as a warning", () => {
    const health = planHealthModel({
      plan_health: {
        state: "past_promise",
        reason: "Past its note promise. Moved 2×.",
        since: "2026-10-01T00:00:00Z",
      },
    });
    expect(health.state).toBe("past_promise");
    expect(health.label).toBe("Past promise");
    expect(health.tone).toBe("warn");
    expect(health.reason).toBe("Past its note promise. Moved 2×.");
    expect(health.since).toBe("2026-10-01T00:00:00Z");
    expect(health.known).toBe(true);
  });

  it.each([
    ["moving", "Moving", "ok"],
    ["no_promise", "No promise", "neutral"],
    ["done", "Done", "ok"],
  ])("maps %s to %s / %s", (state, label, tone) => {
    const health = planHealthModel({ plan_health: { state } });
    expect(health.label).toBe(label);
    expect(health.tone).toBe(tone);
    expect(health.known).toBe(true);
  });

  it("does not read the removed health fields", () => {
    for (const item of [
      { health: { status: "blocked" } },
      { plan_state: { health: "stalled", health_state: "at_risk" } },
      { plan_health: { state: "stale" } },
      { plan_state: null },
      { plan_state: { progress: { done: 7, total: 7 } } },
    ]) {
      const health = planHealthModel(item);
      expect(health.known).toBe(false);
      expect(health.state).toBe("");
    }
  });

  it("renders no badge when core computed nothing", () => {
    const health = planHealthModel({});
    expect(health.known).toBe(false);
    expect(health.state).toBe("");
  });

  it("never produces a label that is truncated", () => {
    for (const state of ATTENTION_ORDER) {
      const health = planHealthModel({ plan_health: { state } });
      expect(health.short).not.toContain("…");
    }
  });
});

describe("nextStepModel", () => {
  it("reads the computed next_step field", () => {
    const next = nextStepModel({
      next_step: {
        id: "ship-it",
        title: "Ship the renderer",
        ref: "card:ship",
      },
    });
    expect(next).toEqual({
      id: "ship-it",
      title: "Ship the renderer",
      ref: "card:ship",
      extra: 0,
    });
  });

  it("prefers the authored plan's title over a humanized id", () => {
    const next = nextStepModel(
      { plan_state: { next_steps: ["ship-it", "then-this"] } },
      { plan: { steps: [{ id: "ship-it", title: "Ship it", ref: "card:s" }] } },
    );
    expect(next.title).toBe("Ship it");
    expect(next.ref).toBe("card:s");
    expect(next.extra).toBe(1);
  });

  it("falls back to the step id read as prose", () => {
    const next = nextStepModel({
      plan_state: { next_steps: ["pick-launch-date"] },
    });
    expect(next.title).toBe("Pick launch date");
    expect(next.ref).toBe("");
  });

  it("is null when there is no next step", () => {
    expect(nextStepModel({})).toBeNull();
    expect(nextStepModel({ plan_state: { next_steps: [] } })).toBeNull();
  });
});

describe("humanizeStepId", () => {
  it("reads a slug as a sentence", () => {
    expect(humanizeStepId("pick-launch-date")).toBe("Pick launch date");
    expect(humanizeStepId("")).toBe("");
  });
});
