import { describe, expect, it } from "vitest";
import { integrationGroups } from "../../src/lib/pm/health.js";

describe("integration coverage", () => {
  it("separates connections, excludes native work and never promotes task done to health", () => {
    const groups = integrationGroups([
      { ref: "card:native", source: { authority: "nexus" } },
      {
        ref: "card:a",
        phase: "done",
        source: { authority: "github", connection_id: "first" },
        freshness: { status: "unknown" },
      },
      {
        ref: "card:b",
        source: { authority: "github", connection_id: "second" },
        refresh: { state: "failed", last_error: "Access revoked" },
      },
    ]);
    expect(groups).toHaveLength(2);
    expect(groups[0].counts.unknown).toBe(1);
    expect(groups[1].counts.error).toBe(1);
  });
});
