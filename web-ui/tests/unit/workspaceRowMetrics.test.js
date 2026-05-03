import { describe, expect, it } from "vitest";

import {
  boardListColumnMetricItems,
  documentListMetricItems,
  topicListLinkedMetricItems,
} from "../../src/lib/workspaceRowMetrics.js";

describe("workspaceRowMetrics", () => {
  describe("topicListLinkedMetricItems", () => {
    it("uses timeline_message_count plus ref list lengths including zeros", () => {
      const items = topicListLinkedMetricItems({
        timeline_message_count: 14,
        document_refs: [],
        board_refs: ["board:a", "board:b"],
      });
      expect(items.map((i) => [i.label, i.count])).toEqual([
        ["Messages", 14],
        ["Documents", 0],
        ["Boards", 2],
      ]);
    });

    it("defaults missing enrichment to zero counts", () => {
      expect(topicListLinkedMetricItems({}).map((i) => i.count)).toEqual([
        0, 0, 0,
      ]);
    });
  });

  describe("boardListColumnMetricItems", () => {
    it("respects canonical column order when no schema is present", () => {
      const items = boardListColumnMetricItems(
        {},
        {
          cards_by_column: { done: 1, backlog: 2 },
        },
      );
      expect(items[0]?.key).toBe("backlog");
      expect(items[0]?.count).toBe(2);
      const done = items.find((i) => i.key === "done");
      expect(done?.count).toBe(1);
    });

    it("uses column_schema order and titles for labels when present", () => {
      const items = boardListColumnMetricItems(
        {
          column_schema: [
            { key: "done", title: "Shipped" },
            { key: "backlog", title: "Ideas" },
          ],
        },
        { cards_by_column: { backlog: 0, done: 4 } },
      );
      expect(items.map((i) => i.label)).toEqual(["Shipped", "Ideas"]);
    });

    it("keeps zero values so rows have a consistent metric shape", () => {
      const items = boardListColumnMetricItems({}, { cards_by_column: {} });
      expect(items.every((i) => i.count === 0)).toBe(true);
      expect(items.map((i) => i.key)).toEqual([
        "backlog",
        "ready",
        "in_progress",
        "blocked",
        "review",
        "done",
      ]);
    });
  });

  describe("documentListMetricItems", () => {
    it("uses explicit list enrichment when present", () => {
      const items = documentListMetricItems({
        timeline_message_count: 2,
        revision_count: 5,
        head_revision_character_count: 120,
      });
      expect(
        items.map((i) => ({ l: i.label, c: i.count, dv: i.displayValue })),
      ).toEqual([
        { l: "Messages", c: 2, dv: undefined },
        { l: "Revisions", c: 5, dv: undefined },
        { l: "Characters", c: 120, dv: undefined },
      ]);
    });

    it("falls back revisions to head_revision_number without revision_count", () => {
      const items = documentListMetricItems({
        head_revision_number: 3,
      });
      const rev = items.find((i) => i.label === "Revisions");
      expect(rev?.count).toBe(3);
    });

    it("shows dash when head character enrichment is omitted", () => {
      const items = documentListMetricItems({ revision_count: 1 });
      const ch = items.find((i) => i.label === "Characters");
      expect(ch?.displayValue).toBe("—");
      expect(ch?.count).toBeUndefined();
    });
  });
});
