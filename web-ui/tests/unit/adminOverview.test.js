import { describe, expect, it } from "vitest";

import {
  countRows,
  formatAgeSeconds,
  telemetryLabel,
  usageMetricCards,
} from "../../src/lib/hosted/adminOverview.js";

describe("admin overview helpers", () => {
  it("keeps unknown telemetry explicit and sorted after known buckets", () => {
    expect(countRows({ unknown: 2, fresh: 5, stale: 1 })).toEqual([
      { key: "fresh", label: "Fresh", value: 5, tone: "ok" },
      { key: "stale", label: "Stale", value: 1, tone: "warn" },
      { key: "unknown", label: "Unknown", value: 2, tone: "warn" },
    ]);
  });

  it("labels stale and unknown telemetry without implying health", () => {
    expect(telemetryLabel("unknown")).toBe("Unknown telemetry");
    expect(telemetryLabel("stale", 7200)).toBe("Stale (2h)");
    expect(formatAgeSeconds(null)).toBe("");
  });

  it("formats global usage cards from byte totals", () => {
    const cards = usageMetricCards({
      storage_bytes: 10 * 1024 * 1024,
      db_bytes: 2 * 1024 * 1024,
      blob_bytes: 8 * 1024 * 1024,
      artifact_count: 1234,
      document_count: 56,
      event_count: 7890,
      agent_count: 12,
      workspace_count: 3,
    });

    expect(cards[0]).toMatchObject({
      label: "Storage",
      value: "10 MB",
      subvalue: "2 MB db / 8 MB blobs",
    });
    expect(cards[1].value).toBe("1,234");
    expect(cards[3].value).toBe("3");
  });
});
