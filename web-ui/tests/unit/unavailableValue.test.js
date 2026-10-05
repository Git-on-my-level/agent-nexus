import { describe, expect, it } from "vitest";

import {
  isUnavailableValue,
  metricValue,
} from "../../src/lib/unavailableValue.js";

describe("isUnavailableValue", () => {
  it("treats nothing-at-all as absent", () => {
    expect(isUnavailableValue(null)).toBe(true);
    expect(isUnavailableValue(undefined)).toBe(true);
    expect(isUnavailableValue("")).toBe(true);
    expect(isUnavailableValue("   ")).toBe(true);
    expect(isUnavailableValue(NaN)).toBe(true);
  });

  it("treats a producer's word for an absence as absent", () => {
    for (const word of [
      "unknown",
      "Unknown",
      "UNAVAILABLE",
      "n/a",
      "None",
      "no data",
      "No recent reading",
      "-",
      "—",
      "?",
    ]) {
      expect(isUnavailableValue(word), word).toBe(true);
    }
  });

  it("keeps a real reading, including zero and false", () => {
    expect(isUnavailableValue(0)).toBe(false);
    expect(isUnavailableValue(false)).toBe(false);
    expect(isUnavailableValue("0")).toBe(false);
    expect(isUnavailableValue("green")).toBe(false);
    expect(isUnavailableValue("3 of 7")).toBe(false);
    expect(isUnavailableValue(94.5)).toBe(false);
  });
});

describe("metricValue", () => {
  it("renders a real value, with its unit", () => {
    expect(metricValue(94, { unit: "%" })).toEqual({
      available: true,
      text: "94 %",
      reason: "",
    });
    expect(metricValue(0).text).toBe("0");
  });

  it("gives a missing value no text, so the caller renders the dash", () => {
    const metric = metricValue(null);
    expect(metric.available).toBe(false);
    expect(metric.text).toBe("");
  });

  it("quotes back the word the producer used, which is the useful reason", () => {
    expect(metricValue("unknown").reason).toContain("unknown");
  });

  it("says plainly when there was nothing at all", () => {
    expect(metricValue(null).reason).toBe("No value reported.");
  });

  it("prefers a reason the caller knows", () => {
    expect(
      metricValue("unknown", { reason: "The last read of this host failed." })
        .reason,
    ).toBe("The last read of this host failed.");
  });
});
