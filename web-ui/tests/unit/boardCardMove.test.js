import { describe, expect, it } from "vitest";

import {
  applyOptimisticCardMove,
  beforeCardIdForInsert,
  buildCardResolvedAttestationEvent,
  cardHasTerminalEvidence,
  filterBoardCardItems,
  sortColumnCards,
} from "../../src/lib/boardCardMove.js";
import { boardCardStableId } from "../../src/lib/boardUtils.js";

describe("boardCardMove", () => {
  it("detects terminal evidence refs", () => {
    expect(cardHasTerminalEvidence({ resolution_refs: ["artifact:a1"] })).toBe(
      true,
    );
    expect(cardHasTerminalEvidence({ resolution_refs: ["event:e1"] })).toBe(
      true,
    );
    expect(cardHasTerminalEvidence({ resolution_refs: ["topic:t1"] })).toBe(
      false,
    );
    expect(
      cardHasTerminalEvidence({ resolution_refs: [] }, ["event:extra"]),
    ).toBe(true);
  });

  it("builds a valid card_resolved attestation event", () => {
    const event = buildCardResolvedAttestationEvent({
      cardItem: {
        membership: {
          id: "card-1",
          handle: "card-1",
          title: "Ship feature",
          thread_id: "thread-1",
        },
      },
      board: { id: "board-1", handle: "board-1" },
      boardId: "board-1",
      note: "Deployed",
    });
    expect(event.type).toBe("card_resolved");
    expect(event.refs).toContain("card:card-1");
    expect(event.refs).toContain("board:board-1");
    expect(event.payload.resolution).toBe("done");
    expect(event.payload.note).toBe("Deployed");
  });

  it("computes before_card_id for insert index", () => {
    expect(beforeCardIdForInsert(["a", "b", "c"], 0)).toBe("a");
    expect(beforeCardIdForInsert(["a", "b", "c"], 1)).toBe("b");
    expect(beforeCardIdForInsert(["a", "b", "c"], 3)).toBe("");
  });

  it("applies optimistic column moves", () => {
    const workspace = {
      cards: {
        items: [
          {
            membership: {
              id: "c1",
              column_key: "ready",
              rank: "10",
              created_at: "2026-01-01T00:00:00Z",
            },
          },
          {
            membership: {
              id: "c2",
              column_key: "ready",
              rank: "20",
              created_at: "2026-01-02T00:00:00Z",
            },
          },
        ],
      },
    };
    const next = applyOptimisticCardMove(workspace, "c1", {
      column_key: "in_progress",
      before_card_id: "",
    });
    expect(next.cards.items[0].membership.column_key).toBe("ready");
    expect(next.cards.items[1].membership.column_key).toBe("in_progress");
    expect(boardCardStableId(next.cards.items[1].membership)).toBe("c1");
  });

  it("filters cards by search and mineOnly", () => {
    const items = [
      {
        membership: {
          id: "c1",
          title: "Alpha",
          assignee_refs: ["actor:me"],
          risk: "high",
        },
      },
      {
        membership: {
          id: "c2",
          title: "Beta",
          assignee_refs: ["actor:other"],
          risk: "low",
        },
      },
    ];
    const mine = filterBoardCardItems(
      items,
      { mineOnly: true, q: "", risk: [], dueFilter: "" },
      "me",
    );
    expect(mine).toHaveLength(1);
    expect(boardCardStableId(mine[0].membership)).toBe("c1");

    const searched = filterBoardCardItems(
      items,
      { q: "beta", mineOnly: false, risk: [], dueFilter: "" },
      "",
    );
    expect(searched).toHaveLength(1);
    expect(boardCardStableId(searched[0].membership)).toBe("c2");
  });

  it("sorts column cards by risk", () => {
    const cards = [
      { membership: { risk: "low", rank: "1" } },
      { membership: { risk: "critical", rank: "2" } },
      { membership: { risk: "medium", rank: "3" } },
    ];
    const sorted = sortColumnCards(cards, "risk");
    expect(sorted[0].membership.risk).toBe("critical");
    expect(sorted[1].membership.risk).toBe("medium");
    expect(sorted[2].membership.risk).toBe("low");
  });
});
