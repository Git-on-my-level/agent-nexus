import { describe, expect, it } from "vitest";

import {
  backupFreshness,
  countRows,
  formatAgeSeconds,
  providerLabels,
  sortRows,
  telemetryLabel,
  usageMetricCards,
  usagePressure,
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

  it("derives drilldown usage pressure without exposing raw internals", () => {
    expect(
      usagePressure({
        usage: { storage_bytes: 95 },
        plan_resolution: { quota: { storage_bytes: 100 } },
      }),
    ).toBe("high");
    expect(usagePressure({ access_mode: "read_only" })).toBe("high");
    expect(usagePressure({ usage: { storage_bytes: 10 } })).toBe("normal");
  });

  it("sorts nested metric columns and labels providers", () => {
    expect(
      sortRows(
        [
          { id: "a", usage: { storage_bytes: 2 } },
          { id: "b", usage: { storage_bytes: 10 } },
        ],
        "usage.storage_bytes",
        "desc",
      ).map((row) => row.id),
    ).toEqual(["b", "a"]);
    expect(providerLabels({ oauth_providers: ["google", "github"] })).toBe(
      "google, github",
    );
  });

  it("keeps backup freshness explicit for drilldown filters", () => {
    expect(
      backupFreshness(
        { last_successful_backup_at: "2026-05-20T00:00:00Z" },
        Date.parse("2026-05-20T12:00:00Z"),
      ),
    ).toBe("fresh");
    expect(backupFreshness({}, Date.parse("2026-05-20T12:00:00Z"))).toBe(
      "unknown",
    );
  });
});
