import { describe, expect, it } from "vitest";

import {
  BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT,
  boardBackingThreadId,
  boardCardHeaderTitle,
  boardCardStableId,
  firstBoardDocumentId,
} from "../../src/lib/boardUtils.js";

describe("boardUtils", () => {
  it("uses 6 rows for board workspace panel previews", () => {
    expect(BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT).toBe(6);
  });

  describe("boardBackingThreadId", () => {
    it("returns thread_id", () => {
      expect(boardBackingThreadId({ thread_id: "thread-a" })).toBe("thread-a");
    });
  });

  describe("firstBoardDocumentId", () => {
    it("prefers document_refs over refs", () => {
      expect(
        firstBoardDocumentId({
          document_refs: ["document:first"],
          refs: ["document:second"],
        }),
      ).toBe("first");
    });

    it("reads document: entries from refs", () => {
      expect(
        firstBoardDocumentId({
          refs: ["thread:t1", "document:runbook-1"],
        }),
      ).toBe("runbook-1");
    });
  });

  describe("boardCardStableId", () => {
    it("prefers versioned card id when present", () => {
      expect(
        boardCardStableId({
          id: "a7472ac6-c002-445b-ade5-b0cc7a2532cd",
          thread_id: null,
        }),
      ).toBe("a7472ac6-c002-445b-ade5-b0cc7a2532cd");
    });

    it("falls back to thread_id for legacy thread-backed rows", () => {
      expect(
        boardCardStableId({
          id: "",
          thread_id: "thread-execution",
        }),
      ).toBe("thread-execution");
    });
  });

  describe("boardCardHeaderTitle", () => {
    it("prefers membership title", () => {
      expect(
        boardCardHeaderTitle(
          { title: "Card A", id: "c1" },
          { title: "Thread T" },
        ),
      ).toBe("Card A");
    });

    it("falls back to thread title", () => {
      expect(
        boardCardHeaderTitle({ title: "", id: "c1" }, { title: "Thread T" }),
      ).toBe("Thread T");
    });

    it("falls back to stable id", () => {
      expect(boardCardHeaderTitle({ title: "", id: "" }, null)).toBe(
        "anon:board-card",
      );
    });
  });
});
