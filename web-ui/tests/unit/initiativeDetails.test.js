import { describe, expect, it } from "vitest";

import {
  CHILD_PREVIEW,
  evidenceLine,
  planModel,
  promiseLine,
  rollupModel,
} from "../../src/lib/initiativeDetails.js";

const NOW = Date.parse("2026-10-15T09:00:00Z");
const child = (n, state, extra = {}) => ({
  ref: `card:c${n}`,
  title: `Child ${n}`,
  stepId: `s${n}`,
  state,
  pastPromise: false,
  needsViewer: false,
  ...extra,
});

describe("promiseLine", () => {
  it("names what was promised and by when", () => {
    expect(
      promiseLine({
        state: "moving",
        promise: { at: "2026-10-21T00:00:00Z", what: "Ship **the fix**" },
      }),
    ).toEqual({
      text: "Ship the fix · by Oct 21",
      field: "initiatives.promise",
      tone: "neutral",
    });
  });

  it("warns once the promise has passed", () => {
    expect(
      promiseLine({
        state: "past_promise",
        promise: { at: "2026-10-06T00:00:00Z", what: "" },
      }).tone,
    ).toBe("warning");
  });

  it("says so when nothing is promised", () => {
    expect(promiseLine({ state: "no_promise", promise: null }).text).toBe(
      "Nothing promised yet",
    );
  });
});

describe("evidenceLine", () => {
  it("dates the latest evidence from the brief", () => {
    expect(
      evidenceLine({ lastEvidenceAt: "2026-10-15T06:00:00Z" }, NOW),
    ).toEqual({
      text: "Last evidence 3h ago",
      field: "initiatives.last_evidence_at",
    });
    expect(evidenceLine({ lastEvidenceAt: "" }, NOW).text).toBe(
      "No evidence yet",
    );
  });
});

describe("rollupModel", () => {
  it("previews the first children with their column and counts the rest", () => {
    const children = [
      child(1, "done"),
      child(2, "in_progress"),
      child(3, "blocked"),
      child(4, "backlog", { needsViewer: true }),
      child(5, "review"),
      child(6, "ready"),
    ];
    const model = rollupModel({
      total: 9,
      byState: { done: 1, in_progress: 1, blocked: 1, backlog: 2, review: 1 },
      pastPromise: 0,
      needingViewer: 1,
      children,
      hasMore: true,
    });
    expect(model.children).toHaveLength(CHILD_PREVIEW);
    expect(model.children.map((c) => c.column)).toEqual([
      "done",
      "moving",
      "stuck",
      "stuck",
      "moving",
    ]);
    expect(model.children[3].stateLabel).toBe("Needs you");
    expect(model.children[1].stateLabel).toBe("In progress");
    expect(model.more).toBe(4);
    expect(model.segments[0]).toMatchObject({ key: "done", label: "done" });
  });

  it("is empty for an initiative with nothing inside", () => {
    expect(rollupModel(undefined)).toMatchObject({
      total: 0,
      children: [],
      more: 0,
      segments: [],
    });
  });
});

describe("planModel", () => {
  const plan = {
    steps: ["a", "b", "c", "d", "e"].map((id) => ({
      id,
      title: `Step ${id}`,
    })),
  };

  it("lists core's ready steps with its own next first, and the owner", () => {
    const model = planModel({
      owner: "actor:luna",
      plan,
      plan_state: { next_steps: ["b", "c", "d", "e"], critical_path: [] },
      summary: { status: { state: "moving" }, next: { id: "d" } },
    });
    expect(model.steps.map((s) => s.id)).toEqual(["d", "b", "c"]);
    expect(model.more).toBe(1);
    expect(model.owner).toBe("actor:luna");
    expect(model.hasPlan).toBe(true);
  });

  it("falls back to the summary's owner and reports a missing plan", () => {
    expect(
      planModel({ work_summary: { status: {}, owner: "actor:sol" } }),
    ).toEqual({ hasPlan: false, steps: [], more: 0, owner: "actor:sol" });
  });

  it("is null before the card is read", () => {
    expect(planModel(null)).toBeNull();
  });
});
