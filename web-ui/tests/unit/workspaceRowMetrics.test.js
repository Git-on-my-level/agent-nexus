import { describe, expect, it } from "vitest";

import {
  boardListColumnMetricItems,
  documentListMetricItems,
} from "../../src/lib/workspaceRowMetrics.js";

describe("workspaceRowMetrics", () => {
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
        { l: "Comments", c: 2, dv: undefined },
        { l: "Versions", c: 5, dv: undefined },
      ]);
    });

    it("falls back versions to head_revision_number without revision_count", () => {
      const items = documentListMetricItems({
        head_revision_number: 3,
      });
      const rev = items.find((i) => i.label === "Versions");
      expect(rev?.count).toBe(3);
    });

    it("never offers a character count", () => {
      const items = documentListMetricItems({
        revision_count: 1,
        head_revision_character_count: 120,
      });
      expect(items.some((i) => i.label === "Characters")).toBe(false);
    });
  });
});
