import { describe, expect, it } from "vitest";

import {
  ATTENTION_ORDER,
  attentionRank,
  humanizeStepId,
  nextStepModel,
  planHealthModel,
  planHealthTitle,
  planStatusMismatch,
} from "../../src/lib/planHealth.js";

describe("attentionRank", () => {
  it("ranks worst first, planless last", () => {
    expect(ATTENTION_ORDER).toEqual([
      "blocked",
      "at_risk",
      "stale",
      "on_track",
      "done",
      "no_plan",
    ]);
    expect(attentionRank("blocked")).toBeLessThan(attentionRank("at_risk"));
    expect(attentionRank("at_risk")).toBeLessThan(attentionRank("stale"));
    expect(attentionRank("stale")).toBeLessThan(attentionRank("on_track"));
    expect(attentionRank("on_track")).toBeLessThan(attentionRank("done"));
    expect(attentionRank("done")).toBeLessThan(attentionRank("no_plan"));
  });

  it("sorts an unknown state with the tail rather than above blocked", () => {
    expect(attentionRank("something-new")).toBeGreaterThanOrEqual(
      attentionRank("no_plan"),
    );
    expect(attentionRank("")).toBeGreaterThanOrEqual(attentionRank("no_plan"));
  });
});

describe("planHealthModel", () => {
  it("reads the computed plan_health field", () => {
    const health = planHealthModel({
      plan_health: {
        state: "at_risk",
        reason: "two steps slipped their due date",
        since: "2026-10-01T00:00:00Z",
      },
    });
    expect(health.state).toBe("at_risk");
    expect(health.short).toBe("At risk");
    expect(health.tone).toBe("warn");
    expect(health.reason).toBe("two steps slipped their due date");
    expect(health.since).toBe("2026-10-01T00:00:00Z");
    expect(health.known).toBe(true);
  });

  it("reads the older health.status field, with stalled as stale", () => {
    expect(
      planHealthModel({ health: { status: "stalled", reason: "no movement" } })
        .state,
    ).toBe("stale");
    expect(planHealthModel({ health: { status: "on_track" } }).state).toBe(
      "on_track",
    );
    expect(planHealthModel({ health: { status: "blocked" } }).state).toBe(
      "blocked",
    );
  });

  it("prefers plan_health over the older field", () => {
    expect(
      planHealthModel({
        plan_health: { state: "blocked" },
        health: { status: "on_track" },
      }).state,
    ).toBe("blocked");
  });

  it("derives done from a finished plan", () => {
    const health = planHealthModel({
      health: { status: "on_track" },
      plan_state: { progress: { done: 7, total: 7 } },
    });
    expect(health.state).toBe("done");
    expect(health.tone).toBe("ok");
  });

  it("does not call a blocked initiative done", () => {
    expect(
      planHealthModel({
        health: { status: "blocked" },
        plan_state: { progress: { done: 7, total: 7 } },
      }).state,
    ).toBe("blocked");
  });

  it("derives no_plan when there is explicitly no plan state", () => {
    const health = planHealthModel({ plan_state: null });
    expect(health.state).toBe("no_plan");
    expect(health.tone).toBe("neutral");
  });

  it("renders no badge when core computed nothing", () => {
    const health = planHealthModel({});
    expect(health.known).toBe(false);
    expect(health.state).toBe("");
  });

  it("never produces a label long enough to be truncated", () => {
    for (const state of ATTENTION_ORDER) {
      const health = planHealthModel({ plan_health: { state } });
      expect(health.short.length).toBeLessThanOrEqual(8);
      expect(health.short).not.toContain("…");
    }
  });
});

describe("planHealthTitle", () => {
  it("joins the label and core's reason", () => {
    expect(
      planHealthTitle({ label: "Stale", reason: "no movement for 9 days" }),
    ).toBe("Stale — no movement for 9 days");
  });

  it("is just the label when there is no reason", () => {
    expect(planHealthTitle({ label: "On track" })).toBe("On track");
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

describe("planStatusMismatch", () => {
  const health = (state) => ({ state });

  it("flags a done card whose plan still has open steps", () => {
    expect(
      planStatusMismatch("done", health("on_track"), { done: 3, total: 7 }),
    ).toContain("4 open steps");
  });

  it("flags a blocked card whose plan reads on track", () => {
    expect(planStatusMismatch("blocked", health("on_track"), null)).toContain(
      "on track",
    );
  });

  it("flags an unblocked card whose plan is blocked", () => {
    expect(
      planStatusMismatch("in_progress", health("blocked"), null),
    ).toContain("plan is blocked");
  });

  it("flags a finished plan on an unfinished card", () => {
    expect(
      planStatusMismatch("in_progress", health("done"), { done: 7, total: 7 }),
    ).toContain("in progress");
  });

  it("says nothing when the two agree", () => {
    expect(
      planStatusMismatch("done", health("done"), { done: 7, total: 7 }),
    ).toBe("");
    expect(
      planStatusMismatch("in_progress", health("on_track"), {
        done: 2,
        total: 7,
      }),
    ).toBe("");
    expect(planStatusMismatch("blocked", health("blocked"), null)).toBe("");
  });

  it("says nothing without both a phase and a computed health", () => {
    expect(planStatusMismatch("", health("blocked"), null)).toBe("");
    expect(planStatusMismatch("done", health(""), null)).toBe("");
  });
});
