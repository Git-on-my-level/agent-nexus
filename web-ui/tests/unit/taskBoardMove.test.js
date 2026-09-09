import { describe, expect, it, vi } from "vitest";
import {
  applyTaskPhaseMove,
  requestedDecisionMap,
} from "../../src/lib/taskBoardMove.js";

describe("task board moves", () => {
  it("moves a Nexus-owned task via cards.move", async () => {
    const coreClient = {
      moveBoardCard: vi.fn().mockResolvedValue({}),
      createPmDecision: vi.fn(),
    };
    const work = {
      ref: "card:local",
      id: "card-local",
      phase: "backlog",
      source: { authority: "nexus" },
    };
    const result = await applyTaskPhaseMove(coreClient, work, "in_progress");
    expect(result.kind).toBe("moved");
    expect(coreClient.moveBoardCard).toHaveBeenCalledWith("", "card-local", {
      column_key: "in_progress",
    });
    expect(coreClient.createPmDecision).not.toHaveBeenCalled();
  });

  it("opens a PM decision for a source-owned task and does not move the card", async () => {
    const coreClient = {
      moveBoardCard: vi.fn(),
      createPmDecision: vi.fn().mockResolvedValue({ id: "decision-1" }),
    };
    const work = {
      ref: "card:gh",
      phase: "review",
      source: { authority: "github", native_id: "12" },
      version: 3,
    };
    const result = await applyTaskPhaseMove(coreClient, work, "done");
    expect(result.kind).toBe("requested");
    expect(coreClient.moveBoardCard).not.toHaveBeenCalled();
    expect(coreClient.createPmDecision).toHaveBeenCalledWith(
      expect.objectContaining({
        instruction: "request status change at GitHub to Done",
        work_ref: "card:gh",
        scope: "work.phase",
      }),
    );
  });
});

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
