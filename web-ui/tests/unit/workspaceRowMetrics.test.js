import { describe, expect, it } from "vitest";

import { documentListMetricItems } from "../../src/lib/workspaceRowMetrics.js";

describe("workspaceRowMetrics", () => {
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
