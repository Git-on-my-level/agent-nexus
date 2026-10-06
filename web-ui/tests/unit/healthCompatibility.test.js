import { describe, expect, it } from "vitest";
import { initiativeTileModel } from "../../src/lib/initiativeTiles.js";
import { planLayout } from "../../src/lib/planShape.js";
import { sinceYouLastLookedStrip } from "../../src/lib/sinceYouLastLooked.js";

describe("health wire compatibility", () => {
  it("prefers canonical state while accepting legacy badges", () => {
    expect(
      initiativeTileModel({ health: { status: "stalled" } }).health.state,
    ).toBe("stale");
    expect(
      initiativeTileModel({
        health: { status: "on_track" },
        plan_health: { state: "no_plan" },
      }).health.state,
    ).toBe("no_plan");
    expect(
      initiativeTileModel({ health: { status: "on_track", state: "at_risk" } })
        .health.state,
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
