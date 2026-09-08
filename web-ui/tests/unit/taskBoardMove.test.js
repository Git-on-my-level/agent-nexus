import { describe, expect, it, vi } from "vitest";
import { applyTaskPhaseMove } from "../../src/lib/taskBoardMove.js";

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
