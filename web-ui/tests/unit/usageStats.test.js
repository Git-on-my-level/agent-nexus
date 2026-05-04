import { describe, expect, it } from "vitest";

import {
  formatStorageBytes,
  pct,
  storageMetric,
} from "../../src/lib/hosted/usageStats.js";

describe("hosted usage stats", () => {
  it("formats small storage in byte units instead of rounding to 1 GB", () => {
    const metric = storageMetric(
      { storage_bytes: 9_400_000, storage_gb: 1 },
      { included_storage_bytes: 256 * 1024 * 1024, included_storage_gb: 1 },
      { storage_bytes_remaining: 256 * 1024 * 1024 - 9_400_000 },
    );

    expect(metric.displayUsed).toBe("9.0 MB");
    expect(metric.displayTotal).toBe("256 MB");
    expect(metric.displayRemaining).toBe("247 MB");
    expect(pct(metric.used, metric.total)).toBe(4);
  });

  it("falls back to legacy GB fields when byte fields are absent", () => {
    const metric = storageMetric(
      { storage_gb: 2 },
      { included_storage_gb: 10 },
      {},
    );

    expect(metric.used).toBe(2 * 1024 * 1024 * 1024);
    expect(metric.displayUsed).toBe("2 GB");
    expect(metric.displayTotal).toBe("10 GB");
  });

  it("formats zero storage clearly", () => {
    expect(formatStorageBytes(0)).toBe("0 B");
  });
});
