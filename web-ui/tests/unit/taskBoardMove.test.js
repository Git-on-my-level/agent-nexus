import { describe, expect, it } from "vitest";
import { requestedDecisionMap } from "../../src/lib/taskBoardMove.js";

describe("requestedDecisionMap", () => {
  const records = [{ ref: "card:gh" }, { handle: "no-ref-work" }];

  it("maps awaiting phase decisions onto tracked work keys", () => {
    const map = requestedDecisionMap(
      [
        {
          id: "d1",
          work_ref: "card:gh",
          status: "awaiting_answer",
          scope: "work.phase",
        },
        {
          id: "d2",
          work_ref: "card:gh",
          status: "answered",
          scope: "work.phase",
        },
        {
          id: "d3",
          work_ref: "card:untracked",
          status: "awaiting_answer",
          scope: "work.phase",
        },
      ],
      records,
    );
    expect(map).toEqual({ "card:gh": "d1" });
  });

  it("matches status-change instructions without scope", () => {
    const map = requestedDecisionMap(
      [
        {
          id: "d4",
          work_ref: "card:gh",
          status: "awaiting_answer",
          instruction: "request status change at GitHub to Done",
        },
      ],
      records,
    );
    expect(map).toEqual({ "card:gh": "d4" });
  });

  it("ignores awaiting decisions that are not phase requests", () => {
    const map = requestedDecisionMap(
      [
        {
          id: "d5",
          work_ref: "card:gh",
          status: "awaiting_answer",
          scope: "other",
          instruction: "draft the weekly update",
        },
      ],
      records,
    );
    expect(map).toEqual({});
  });
});
