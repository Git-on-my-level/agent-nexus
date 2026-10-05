import { describe, expect, it } from "vitest";

import {
  groupedInitiativeTiles,
  inboxWaitingLine,
  initiativeTileModel,
  initiativeTiles,
  planSegments,
  tileGroup,
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
  // A planned initiative is the default; a planless one passes
  // `plan_state: null` so the distinction is visible in the test that wants it.
  plan_state: {
    steps: [{ id: "spec", status: "done" }],
    progress: { done: 2, total: 5 },
  },
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

  it("carries the name and the link to the initiative page", () => {
    const tile = model();
    expect(tile.title).toBe("Release B");
    expect(tile.href).toBe("/w/card%3Arelease-b");
  });

  it("shows the description as plain prose, never the markdown source", () => {
    const tile = model({ summary: "**Goal:** ship `anx` by Friday" });
    expect(tile.excerpt).toBe("Goal: ship anx by Friday");
  });

  it("clips a long description rather than wrapping a tile", () => {
    const tile = model({ summary: "word ".repeat(80) }, { excerptLimit: 40 });
    expect(tile.excerpt.length).toBeLessThanOrEqual(41);
    expect(tile.excerpt.endsWith("…")).toBe(true);
  });

  it("reads health through the shared vocabulary", () => {
    expect(model({ health: { status: "stalled" } }).health).toMatchObject({
      state: "stale",
      short: "Stale",
      tone: "warn",
    });
    expect(model({ health: { status: "blocked" } }).health.tone).toBe("danger");
    expect(model({ health: { status: "on_track" } }).health.tone).toBe("ok");
  });

  it("reads the computed plan_health field when core sends it", () => {
    const tile = model({
      plan_health: { state: "at_risk", reason: "two steps slipped" },
    });
    expect(tile.health.state).toBe("at_risk");
    expect(tile.health.reason).toBe("two steps slipped");
  });

  it("calls an explicitly planless row No plan", () => {
    expect(model({ plan_state: null }).health).toMatchObject({
      state: "no_plan",
      short: "No plan",
      known: true,
    });
  });

  it("has no health badge for a row core computed nothing for", () => {
    // Neither a health field nor a plan_state key: core has not said.
    const tile = initiativeTileModel(
      { ref: "card:x", title: "X" },
      { now: NOW },
    );
    expect(tile.health.known).toBe(false);
    expect(tile.group).toBe("no_plan");
  });

  it("groups a done initiative away from the ones needing attention", () => {
    expect(
      model({
        health: { status: "on_track" },
        plan_state: planState({ progress: { done: 3, total: 3 } }),
      }).group,
    ).toBe("done");
    expect(model({ plan_state: null }).group).toBe("no_plan");
    expect(model({ health: { status: "blocked" } }).group).toBe("attention");
  });

  it("names the shape, since the tile draws a bar rather than the graph", () => {
    expect(model({ plan_state: planState() }).shapeLabel).toBe("Timeline");
    expect(model({ plan_state: planState({ shape: "dag" }) }).shapeLabel).toBe(
      "Tech tree",
    );
    expect(
      model({ plan_state: planState({ shape: "lanes" }) }).shapeLabel,
    ).toBe("Lanes");
  });

  it("takes progress from the projection", () => {
    expect(model({ plan_state: null }).progress).toEqual({ done: 2, total: 5 });
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
    expect(
      model({ progress: { done: 0, total: 0 }, plan_state: null }).progress,
    ).toBeNull();
  });

  it("names the next step and counts the rest", () => {
    const tile = model({ plan_state: planState() });
    expect(tile.next.title).toBe("Build it");
    expect(tile.next.extra).toBe(0);

    const more = model({
      plan_state: planState({ next_steps: ["build-it", "draft", "review"] }),
    });
    expect(more.next.title).toBe("Build it");
    expect(more.next.extra).toBe(2);
  });

  it("surfaces blocked step titles as the needs-you pill", () => {
    const tile = model({
      needs: ["Panel binding decision", "Adapter contract"],
      health: { status: "blocked" },
    });
    expect(tile.needs).toEqual(["Panel binding decision", "Adapter contract"]);
  });

  it("prefers plan movement over the card timestamp for the age badge", () => {
    expect(model({ plan_state: planState() }).movedAt).toBe(ago(2 * 3_600_000));
  });

  it("falls back to the card timestamp when there is no plan", () => {
    expect(model({ plan_state: null }).movedAt).toBe(ago(3 * 3_600_000));
  });

  it("says a card has no plan when the projection sends none", () => {
    expect(model({ plan_state: null }).hasPlan).toBe(false);
    expect(model({ plan_state: planState() }).hasPlan).toBe(true);
  });

  it("keeps a blocked planless initiative urgent rather than filing it away", () => {
    // Core derives a planless initiative's status from its native phase. A
    // blocked one still needs a decision, plan or no plan.
    const tile = model({ plan_state: null, health: { status: "blocked" } });
    expect(tile.health.state).toBe("blocked");
    expect(tile.group).toBe("attention");
  });
});

describe("tileGroup", () => {
  it("sends done and planless initiatives to their own blocks", () => {
    expect(tileGroup("done")).toBe("done");
    expect(tileGroup("no_plan")).toBe("no_plan");
    expect(tileGroup("")).toBe("no_plan");
    for (const state of ["blocked", "at_risk", "stale", "on_track"]) {
      expect(tileGroup(state)).toBe("attention");
    }
  });
});

describe("initiativeTiles", () => {
  it("sorts by attention, worst first", () => {
    const tiles = initiativeTiles(
      [
        row({ ref: "card:ok", health: { status: "on_track" } }),
        row({ ref: "card:stale", health: { status: "stalled" } }),
        row({ ref: "card:blocked", health: { status: "blocked" } }),
        row({ ref: "card:risk", plan_health: { state: "at_risk" } }),
      ],
      { now: NOW },
    );
    expect(tiles.map((tile) => tile.ref)).toEqual([
      "card:blocked",
      "card:risk",
      "card:stale",
      "card:ok",
    ]);
  });

  it("keeps the projection's order within one health state", () => {
    const tiles = initiativeTiles(
      [
        row({ ref: "card:a", health: { status: "blocked" } }),
        row({ ref: "card:b", health: { status: "blocked" } }),
        row({ ref: "card:c", health: { status: "blocked" } }),
      ],
      { now: NOW },
    );
    expect(tiles.map((tile) => tile.ref)).toEqual([
      "card:a",
      "card:b",
      "card:c",
    ]);
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

describe("groupedInitiativeTiles", () => {
  it("splits the grid from the collapsed tails", () => {
    const grouped = groupedInitiativeTiles(
      [
        row({ ref: "card:blocked", health: { status: "blocked" } }),
        row({ ref: "card:ok", health: { status: "on_track" } }),
        row({ ref: "card:none", plan_state: null }),
        row({
          ref: "card:done",
          health: { status: "on_track" },
          plan_state: planState({ progress: { done: 3, total: 3 } }),
        }),
      ],
      { now: NOW },
    );
    expect(grouped.attention.map((tile) => tile.ref)).toEqual([
      "card:blocked",
      "card:ok",
    ]);
    expect(grouped.done.map((tile) => tile.ref)).toEqual(["card:done"]);
    expect(grouped.noPlan.map((tile) => tile.ref)).toEqual(["card:none"]);
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
