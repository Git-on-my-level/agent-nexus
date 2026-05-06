import { describe, expect, it } from "vitest";

import {
  BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT,
  boardBackingThreadId,
  boardCardHeaderTitle,
  boardCardStableId,
  boardCardTimelineMessageCount,
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
    it("prefers public card handle when present", () => {
      expect(
        boardCardStableId({
          id: "a7472ac6-c002-445b-ade5-b0cc7a2532cd",
          ref: "card:launch-checklist",
          handle: "launch-checklist",
          thread_id: null,
        }),
      ).toBe("launch-checklist");
    });

    it("does not expose internal UUID ids", () => {
      expect(
        boardCardStableId({
          id: "a7472ac6-c002-445b-ade5-b0cc7a2532cd",
          thread_id: "",
        }),
      ).toBe("anon:board-card");
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

  describe("boardCardTimelineMessageCount", () => {
    it("reads derived.timeline_message_count", () => {
      expect(
        boardCardTimelineMessageCount({
          derived: { timeline_message_count: 4 },
        }),
      ).toBe(4);
    });

    it("tolerates camelCase and missing derived", () => {
      expect(
        boardCardTimelineMessageCount({
          derived: { timelineMessageCount: 2 },
        }),
      ).toBe(2);
      expect(boardCardTimelineMessageCount({})).toBe(0);
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
