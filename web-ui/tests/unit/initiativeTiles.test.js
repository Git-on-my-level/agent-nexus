import { describe, expect, it } from "vitest";

import {
  humanizeStepId,
  inboxWaitingLine,
  initiativeTileModel,
  initiativeTiles,
  planSegments,
} from "../../src/lib/initiativeTiles.js";

const NOW = Date.parse("2026-10-04T12:00:00Z");
const ago = (ms) => new Date(NOW - ms).toISOString();

/** A live initiatives row shaped as core's projection sends it. */
const row = (overrides = {}) => ({
  ref: "card:release-b",
  title: "Release B",
  summary: "Initiative plans, live dashboards and agent ergonomics.",
  priority: "high",
  phase: "in_progress",
  progress: { done: 2, total: 5 },
  needs: [],
  updated_at: ago(3 * 3_600_000),
  ...overrides,
});

const planState = (overrides = {}) => ({
  steps: [
    { id: "spec", status: "done", resolvable: true },
    { id: "build-it", status: "active", resolvable: true },
    { id: "ship", status: "not_started", resolvable: false },
  ],
  progress: { done: 1, total: 3 },
  critical_path: ["build-it", "ship"],
  next_steps: ["build-it"],
  shape: "chain",
  health: "on_track",
  last_movement_at: ago(2 * 3_600_000),
  ...overrides,
});

describe("humanizeStepId", () => {
  it("reads a slug as prose", () => {
    expect(humanizeStepId("computed-progress-and-health")).toBe(
      "Computed progress and health",
    );
    expect(humanizeStepId("ship")).toBe("Ship");
  });

  it("returns nothing for an empty id", () => {
    expect(humanizeStepId("")).toBe("");
    expect(humanizeStepId(null)).toBe("");
  });
});

describe("planSegments", () => {
  it("gives one segment per step, flagging the critical path", () => {
    const { segments, overflow } = planSegments(planState());
    expect(segments.map((segment) => segment.status)).toEqual([
      "done",
      "active",
      "not_started",
    ]);
    expect(segments.map((segment) => segment.onCriticalPath)).toEqual([
      false,
      true,
      true,
    ]);
    expect(overflow).toBe(0);
  });

  it("caps a long plan and reports the overflow", () => {
    const steps = Array.from({ length: 30 }, (_, index) => ({
      id: `s${index}`,
      status: "not_started",
    }));
    const { segments, overflow } = planSegments({ steps }, 24);
    expect(segments).toHaveLength(24);
    expect(overflow).toBe(6);
  });

  it("returns nothing without a plan state", () => {
    expect(planSegments(null)).toEqual({ segments: [], overflow: 0 });
  });
});

describe("initiativeTileModel", () => {
  const model = (overrides, options) =>
    initiativeTileModel(row(overrides), {
      now: NOW,
      href: (ref) => `/w/${encodeURIComponent(ref)}`,
      ...options,
    });

  it("carries the name, status line and link to the initiative page", () => {
    const tile = model();
    expect(tile.title).toBe("Release B");
    expect(tile.status).toBe(
      "Initiative plans, live dashboards and agent ergonomics.",
    );
    expect(tile.href).toBe("/w/card%3Arelease-b");
  });

  it("shows health as a badge when the projection computes one", () => {
    expect(model({ health: "stalled" })).toMatchObject({
      healthLabel: "Stalled",
      healthTone: "warn",
    });
    expect(model({ health: "blocked" }).healthTone).toBe("danger");
    expect(model({ health: "on_track" }).healthTone).toBe("ok");
  });

  it("has no health badge for a card with no plan", () => {
    expect(model().healthLabel).toBe("");
  });

  it("names the shape so a reader knows what the mini-viz is", () => {
    expect(model({ plan_state: planState() }).shapeLabel).toBe("Timeline");
    expect(model({ plan_state: planState({ shape: "dag" }) }).shapeLabel).toBe(
      "Tech tree",
    );
    expect(
      model({ plan_state: planState({ shape: "lanes" }) }).shapeLabel,
    ).toBe("Lanes");
  });

  it("takes progress from the projection", () => {
    expect(model().progress).toEqual({ done: 2, total: 5 });
  });

  it("prefers the plan's progress, which is what the bar is drawn from", () => {
    // The segments come from plan_state.steps, so the count beside them has to
    // describe the same steps rather than the row's own tally.
    const tile = model({
      progress: { done: 1, total: 6 },
      plan_state: planState(),
    });
    expect(tile.progress).toEqual({ done: 1, total: 3 });
    expect(tile.segments).toHaveLength(3);
  });

  it("drops a progress of zero total rather than drawing an empty bar", () => {
    expect(model({ progress: { done: 0, total: 0 } }).progress).toBeNull();
  });

  it("names the next step from its id, which is all the projection carries", () => {
    const tile = model({ plan_state: planState() });
    expect(tile.next).toBe("Build it");
    expect(tile.extraNext).toBe(0);
  });

  it("counts further next steps rather than listing them", () => {
    const tile = model({
      plan_state: planState({ next_steps: ["build-it", "draft", "review"] }),
    });
    expect(tile.next).toBe("Build it");
    expect(tile.extraNext).toBe(2);
  });

  it("surfaces blocked step titles as the needs-you pill", () => {
    const tile = model({
      needs: ["Panel binding decision", "Adapter contract"],
      health: "blocked",
    });
    expect(tile.needs).toEqual(["Panel binding decision", "Adapter contract"]);
  });

  it("prefers plan movement over the card timestamp for moved-ago", () => {
    const tile = model({ plan_state: planState() });
    expect(tile.movedLabel).toBe("2h ago");
  });

  it("falls back to the card timestamp when there is no plan", () => {
    expect(model().movedLabel).toBe("3h ago");
  });

  it("says a card has no plan when the projection sends none", () => {
    expect(model().hasPlan).toBe(false);
    expect(model({ plan_state: planState() }).hasPlan).toBe(true);
  });
});

describe("initiativeTiles", () => {
  it("keeps projection order", () => {
    const tiles = initiativeTiles(
      [row({ ref: "card:a" }), row({ ref: "card:b" })],
      { now: NOW },
    );
    expect(tiles.map((tile) => tile.ref)).toEqual(["card:a", "card:b"]);
  });

  it("skips a row with no ref rather than rendering a dead tile", () => {
    expect(
      initiativeTiles([row({ ref: "" }), row()], { now: NOW }),
    ).toHaveLength(1);
  });

  it("tolerates a missing list", () => {
    expect(initiativeTiles(undefined)).toEqual([]);
    expect(initiativeTiles(null)).toEqual([]);
  });
});

describe("inboxWaitingLine", () => {
  it("summarises what is waiting as one line, without calling it all decisions", () => {
    expect(
      inboxWaitingLine({ status: "ok", count: 3, href: "/inbox" }),
    ).toEqual({
      count: 3,
      href: "/inbox",
      label: "3 items need you",
    });
  });

  it("says item in the singular", () => {
    expect(
      inboxWaitingLine({ status: "ok", count: 1, href: "/inbox" }).label,
    ).toBe("1 item needs you");
  });

  it("marks a truncated count", () => {
    expect(
      inboxWaitingLine({ status: "ok", count: 50, truncated: true, href: "/i" })
        .label,
    ).toBe("50+ items need you");
  });

  it("shows nothing when nothing waits or the read failed", () => {
    expect(inboxWaitingLine({ status: "ok", count: 0 })).toBeNull();
    expect(inboxWaitingLine({ status: "unavailable", count: 4 })).toBeNull();
    expect(inboxWaitingLine(null)).toBeNull();
  });
});
