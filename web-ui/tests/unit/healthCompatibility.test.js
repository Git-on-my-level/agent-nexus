import { describe, expect, it } from "vitest";
import { workSummaryModel } from "../../src/lib/workSummary.js";
import { planLayout } from "../../src/lib/planShape.js";
import { sinceYouLastLookedStrip } from "../../src/lib/sinceYouLastLooked.js";

describe("health wire compatibility", () => {
  it("prefers the computed summary over every legacy spelling", () => {
    // Core's `work_summary` is the field the UI renders; the older fields
    // remain on the wire and must not override it.
    expect(
      workSummaryModel({
        health: { status: "on_track" },
        plan_health: { state: "stale" },
        work_summary: { status: { state: "blocked", label: "Blocked" } },
      }).status.state,
    ).toBe("blocked");
  });

  it("prefers canonical state while accepting legacy badges", () => {
    expect(
      workSummaryModel({ health: { status: "stalled" } }).status.state,
    ).toBe("stale");
    expect(
      workSummaryModel({
        health: { status: "on_track" },
        plan_health: { state: "no_plan" },
      }).status.state,
    ).toBe("no_plan");
    expect(
      workSummaryModel({ health: { status: "on_track", state: "at_risk" } })
        .status.state,
    ).toBe("at_risk");
  });

  it("reads both legacy and canonical plan-state fields", () => {
    const plan = { steps: [{ id: "ship", title: "Ship" }] };
    expect(planLayout(plan, { planState: { health: "stalled" } }).health).toBe(
      "stale",
    );
    expect(
      planLayout(plan, {
        planState: { health: "on_track", health_state: "done" },
      }).health,
    ).toBe("done");
  });

  it("renders both stale digest spellings as the same attention event", () => {
    const digest = (kind) => ({
      since: "2026-10-05T00:00:00Z",
      items: [{ kind, ref: "card:ship", title: "Ship" }],
    });
    expect(sinceYouLastLookedStrip(digest("initiative_stalled"))).toEqual(
      sinceYouLastLookedStrip(digest("initiative_stale")),
    );
  });
});
