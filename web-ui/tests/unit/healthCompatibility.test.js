import { describe, expect, it } from "vitest";
import { workSummaryModel } from "../../src/lib/workSummary.js";
import { sinceYouLastLookedStrip } from "../../src/lib/sinceYouLastLooked.js";

describe("health wire compatibility", () => {
  it("prefers the computed summary over plan_health", () => {
    expect(
      workSummaryModel({
        plan_health: { state: "past_promise" },
        work_summary: { status: { state: "moving", label: "Moving" } },
      }).status.state,
    ).toBe("moving");
  });

  it("reads plan_health when there is no computed summary", () => {
    expect(
      workSummaryModel({ plan_health: { state: "past_promise" } }).status.state,
    ).toBe("past_promise");
    expect(
      workSummaryModel({ plan_health: { state: "no_promise" } }).status.state,
    ).toBe("no_promise");
  });

  it("ignores the removed health fields", () => {
    expect(
      workSummaryModel({ health: { status: "stalled" } }).status.state,
    ).toBe("unknown");
    expect(
      workSummaryModel({ plan_state: { health: "at_risk", health_state: "x" } })
        .status.state,
    ).toBe("unknown");
  });

  it("drops the removed stale digest spellings", () => {
    const digest = (kind) => ({
      since: "2026-10-05T00:00:00Z",
      items: [{ kind, ref: "card:ship", title: "Ship" }],
    });
    expect(sinceYouLastLookedStrip(digest("initiative_stalled"))).toBeNull();
    expect(sinceYouLastLookedStrip(digest("initiative_stale"))).toBeNull();
  });
});
