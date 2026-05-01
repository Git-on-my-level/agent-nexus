import { describe, expect, it } from "vitest";

import {
  ENTERPRISE_SALES_HREF,
  PLAN_CARDS,
  planBadgeClasses,
  planLabel,
  tierEnvelopeForBillingSummary,
  usagePlanLimitFeatureLines,
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

  it("usagePlanLimitFeatureLines derives bullets from envelope fields", () => {
    expect(
      usagePlanLimitFeatureLines({ workspace_limit: 5, artifact_capacity: 125_000 }),
    ).toEqual(["Up to 5 workspaces", "125,000 artifacts included"]);
  });

  it("usagePlanLimitFeatureLines parses numeric strings from JSON edge cases", () => {
    expect(
      usagePlanLimitFeatureLines({
        workspace_limit: "5",
        artifact_capacity: "125000",
      }),
    ).toEqual(["Up to 5 workspaces", "125,000 artifacts included"]);
  });

  it("usagePlanLimitFeatureLines returns empty when incomplete", () => {
    expect(usagePlanLimitFeatureLines({})).toEqual([]);
    expect(
      usagePlanLimitFeatureLines({ workspace_limit: 1 }),
    ).toEqual([]);
  });

  it("tierEnvelopeForBillingSummary prefers plan_usage_envelopes", () => {
    const summary = {
      plan_usage_envelopes: {
        team: {
          workspace_limit: 99,
          artifact_capacity: 42,
        },
      },
    };
    expect(tierEnvelopeForBillingSummary(summary, "team")).toEqual({
      workspace_limit: 99,
      artifact_capacity: 42,
    });
  });

  it("tierEnvelopeForBillingSummary falls back when tiers missing", () => {
    expect(tierEnvelopeForBillingSummary({}, "starter")).toEqual({
      workspace_limit: 1,
      artifact_capacity: 1000,
    });
  });
});
