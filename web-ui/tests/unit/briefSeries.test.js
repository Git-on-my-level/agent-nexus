import { describe, expect, it } from "vitest";
import { normalizeBrief } from "../../src/lib/brief.js";

describe("initiative series normalization", () => {
  it("passes series and metricFlat through without losing zero or false", () => {
    const series = [
      {
        ref: "series:cost?loop=one",
        name: "cost",
        labels: { loop: "one" },
        last_value: 0,
        last_at: "2026-10-15T00:00:00Z",
      },
    ];
    const metricFlat = { flat: false, for_seconds: 0, window_seconds: 604800 };
    const [initiative] = normalizeBrief({
      initiatives: [{ ref: "card:one", series, metric_flat: metricFlat }],
    }).initiatives;
    expect(initiative.series).toEqual(series);
    expect(initiative.metricFlat).toEqual(metricFlat);
  });

  it("defaults older payloads and null signals safely", () => {
    const rows = normalizeBrief({
      initiatives: [
        { ref: "card:old" },
        { ref: "card:none", series: [], metric_flat: null },
      ],
    }).initiatives;
    for (const row of rows) {
      expect(row.series).toEqual([]);
      expect(row.metricFlat).toBeNull();
    }
  });
});
