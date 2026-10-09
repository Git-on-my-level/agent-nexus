import { describe, expect, it, vi } from "vitest";
import {
  applyTaskPhaseMove,
  requestedDecisionMap,
} from "../../src/lib/taskBoardMove.js";

describe("task board moves", () => {
  it("moves a Nexus-owned task via cards.move", async () => {
    const coreClient = {
      getBoard: vi
        .fn()
        .mockResolvedValue({ board: { updated_at: "2026-09-12T10:00:00Z" } }),
      moveBoardCard: vi.fn().mockResolvedValue({}),
      createPmDecision: vi.fn(),
    };
    const work = {
      ref: "card:local",
      board_ref: "board:studio",
      phase: "backlog",
      source: { authority: "nexus" },
    };
    const result = await applyTaskPhaseMove(coreClient, work, "in_progress");
    expect(result.kind).toBe("moved");
    expect(coreClient.getBoard).toHaveBeenCalledWith("board:studio");
    expect(coreClient.moveBoardCard).toHaveBeenCalledWith(
      "board:studio",
      "card:local",
      {
        column_key: "in_progress",
        if_board_updated_at: "2026-09-12T10:00:00Z",
      },
    );
    expect(coreClient.createPmDecision).not.toHaveBeenCalled();
  });

  it("reorders a Nexus-owned task in the same column via before_card_id", async () => {
    const coreClient = {
      getBoard: vi
        .fn()
        .mockResolvedValue({ board: { updated_at: "2026-09-12T10:00:00Z" } }),
      moveBoardCard: vi.fn().mockResolvedValue({}),
      createPmDecision: vi.fn(),
    };
    const work = {
      ref: "card:local",
      board_ref: "board:studio",
      phase: "ready",
      source: { authority: "nexus" },
    };
    const result = await applyTaskPhaseMove(coreClient, work, "ready", {
      beforeCardId: "card:peer",
    });
    expect(result.kind).toBe("moved");
    expect(coreClient.moveBoardCard).toHaveBeenCalledWith(
      "board:studio",
      "card:local",
      {
        column_key: "ready",
        if_board_updated_at: "2026-09-12T10:00:00Z",
        before_card_id: "card:peer",
      },
    );
  });

  it("refuses to move a Nexus-owned task that is not on a board", async () => {
    const coreClient = {
      getBoard: vi.fn(),
      moveBoardCard: vi.fn(),
      createPmDecision: vi.fn(),
    };
    const work = {
      ref: "card:loose",
      phase: "backlog",
      source: { authority: "nexus" },
    };
    await expect(applyTaskPhaseMove(coreClient, work, "ready")).rejects.toThrow(
      /not on a board/,
    );
    expect(coreClient.moveBoardCard).not.toHaveBeenCalled();
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
    // A done request needs evidence wherever the work lives.
    expect((await applyTaskPhaseMove(coreClient, work, "done")).kind).toBe(
      "needs_evidence",
    );
    const result = await applyTaskPhaseMove(coreClient, work, "done", {
      resolutionRefs: ["artifact:proof"],
    });
    expect(result.kind).toBe("requested");
    expect(coreClient.moveBoardCard).not.toHaveBeenCalled();
    expect(coreClient.createPmDecision).toHaveBeenCalledWith(
      expect.objectContaining({
        instruction: "request status change at GitHub to Done",
        payload: { phase: "done", resolution_refs: ["artifact:proof"] },
        work_ref: "card:gh",
        scope: "work.phase",
      }),
    );
  });
});

describe("a source-owned move with no PM agent onboarded", () => {
  const sourceOwned = {
    ref: "card:vendor",
    board_ref: "board:studio",
    phase: "backlog",
    source: { authority: "github", native_id: "12" },
  };

  /*
   * The request is a PM proposal: the PM carries it out at the source and the
   * reader answers it in the Inbox. A PM agent runs on the reader's own
   * computer, so a workspace can have none — and a proposal filed then has
   * nobody to carry it out. Refuse before writing rather than leaving one
   * waiting forever.
   */
  it("refuses before writing anything", async () => {
    const coreClient = {
      getBoard: vi.fn(),
      moveBoardCard: vi.fn(),
      createPmDecision: vi.fn(),
    };
    const result = await applyTaskPhaseMove(
      coreClient,
      sourceOwned,
      "in_progress",
      { pmOnboarded: false },
    );
    expect(result).toMatchObject({ kind: "needs_pm" });
    expect(coreClient.createPmDecision).not.toHaveBeenCalled();
    expect(coreClient.moveBoardCard).not.toHaveBeenCalled();
  });

  it("still files the proposal when a PM exists", async () => {
    const coreClient = {
      getBoard: vi.fn(),
      moveBoardCard: vi.fn(),
      createPmDecision: vi.fn().mockResolvedValue({ id: "decision-1" }),
    };
    const result = await applyTaskPhaseMove(
      coreClient,
      sourceOwned,
      "in_progress",
      { pmOnboarded: true },
    );
    expect(result).toMatchObject({ kind: "requested" });
    expect(coreClient.createPmDecision).toHaveBeenCalledTimes(1);
  });

  // A Nexus-owned move is the UI's own write and needs no PM at all.
  it("does not touch a Nexus-owned move", async () => {
    const coreClient = {
      getBoard: vi
        .fn()
        .mockResolvedValue({ board: { updated_at: "2026-09-12T10:00:00Z" } }),
      moveBoardCard: vi.fn().mockResolvedValue({}),
      createPmDecision: vi.fn(),
    };
    const result = await applyTaskPhaseMove(
      coreClient,
      { ...sourceOwned, source: { authority: "nexus" } },
      "in_progress",
      { pmOnboarded: false },
    );
    expect(result.kind).toBe("moved");
    expect(coreClient.moveBoardCard).toHaveBeenCalledTimes(1);
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
