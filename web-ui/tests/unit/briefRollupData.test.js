import { describe, expect, it } from "vitest";
import { BRIEF_COMPUTED_FIELDS, normalizeBrief } from "../../src/lib/brief.js";

describe("Watch computed rollup data", () => {
  it("preserves outcome, proposed placement, workstream metrics and completion dates", () => {
    const leaf = {
      ref: "card:leaf",
      title: "Measure",
      state: "done",
      owner: "actor:alex",
      done_at: "2026-10-25T12:00:00Z",
      short_code: "fs-p3",
    };
    const stream = {
      ref: "card:stream",
      state: "ready",
      rollup: {
        total: 1,
        by_state: { done: 1 },
        earliest_promise_at: "2026-10-27T12:00:00Z",
        children: [leaf],
      },
    };
    const brief = normalizeBrief({
      initiatives: [
        {
          ref: "card:root",
          outcome: "Ship a usable board",
          proposed: true,
          rollup: { total: 1, children: [stream], workstreams: [stream] },
        },
      ],
    });
    const initiative = brief.initiatives[0];
    expect(initiative.outcome).toBe("Ship a usable board");
    expect(initiative.proposed).toBe(true);
    const rollup = initiative.rollup.workstreams[0].rollup;
    expect(rollup.total).toBe(1);
    expect(rollup.byState).toEqual({ done: 1 });
    expect(rollup.earliestPromiseAt).toBe("2026-10-27T12:00:00Z");
    expect(rollup.children[0]).toMatchObject({
      owner: "actor:alex",
      doneAt: leaf.done_at,
      shortCode: "fs-p3",
      rollup: null,
    });
    for (const field of [
      "initiatives.outcome",
      "initiatives.proposed",
      "initiatives.rollup.earliest_promise_at",
      "initiatives.rollup.workstreams.rollup.total",
      "initiatives.rollup.children.done_at",
    ])
      expect(BRIEF_COMPUTED_FIELDS.has(field)).toBe(true);
  });
});
