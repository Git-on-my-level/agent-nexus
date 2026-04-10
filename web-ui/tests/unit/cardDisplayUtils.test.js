import { describe, expect, it } from "vitest";

import { resolvePriorityBadge } from "../../src/lib/cardDisplayUtils.js";
import { getPriorityLabel } from "../../src/lib/topicFilters.js";

describe("cardDisplayUtils", () => {
  describe("resolvePriorityBadge", () => {
    it("returns null for empty priority", () => {
      expect(resolvePriorityBadge("", getPriorityLabel)).toBe(null);
    });

    it("maps p0–p3 to fixed labels", () => {
      expect(resolvePriorityBadge("p0", getPriorityLabel)?.label).toBe("P0");
      expect(resolvePriorityBadge("p3", getPriorityLabel)?.label).toBe("P3");
    });

    it("delegates other values to getPriorityLabel", () => {
      const b = resolvePriorityBadge("high", getPriorityLabel);
      expect(b?.label).toBe(getPriorityLabel("high"));
    });
  });
});
