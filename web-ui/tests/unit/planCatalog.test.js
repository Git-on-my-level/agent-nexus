import { describe, expect, it } from "vitest";

import {
  ENTERPRISE_SALES_HREF,
  PLAN_CARDS,
  planBadgeClasses,
  planLabel,
} from "../../src/lib/hosted/planCatalog.js";

describe("planCatalog", () => {
  it("planLabel matches control-plane display names", () => {
    expect(planLabel("starter")).toBe("Free");
    expect(planLabel("team")).toBe("Pro");
    expect(planLabel("scale")).toBe("Scale");
    expect(planLabel("enterprise")).toBe("Enterprise");
  });

  it("ENTERPRISE_SALES_HREF points at scalingforever.com", () => {
    expect(ENTERPRISE_SALES_HREF).toMatch(/^mailto:sales@scalingforever\.com/);
  });

  it("PLAN_CARDS have stable tier ids and no per-seat price suffixes", () => {
    const ids = PLAN_CARDS.map((c) => c.id);
    expect(ids).toEqual(["starter", "team", "scale", "enterprise"]);
    for (const c of PLAN_CARDS) {
      if (c.priceSuffix) {
        expect(String(c.priceSuffix)).not.toMatch(/seat/i);
      }
    }
  });

  it("planBadgeClasses returns theme strings", () => {
    expect(planBadgeClasses("enterprise")).toContain("fuchsia");
    expect(planBadgeClasses("team")).toContain("ok-text");
  });
});
