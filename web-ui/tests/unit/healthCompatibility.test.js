import { describe, expect, it } from "vitest";
import { workSummaryModel } from "../../src/lib/workSummary.js";

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
});
