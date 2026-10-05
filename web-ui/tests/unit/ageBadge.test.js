import { describe, expect, it } from "vitest";

import { ageTitle, formatAge } from "../../src/lib/ageBadge.js";

const now = Date.parse("2026-10-04T12:00:00Z");
const ago = (ms) => new Date(now - ms).toISOString();

describe("formatAge", () => {
  it("reads in minutes, hours, days, weeks and years", () => {
    expect(formatAge(ago(30_000), now)).toBe("now");
    expect(formatAge(ago(8 * 60_000), now)).toBe("8m");
    expect(formatAge(ago(8 * 3_600_000), now)).toBe("8h");
    expect(formatAge(ago(3 * 86_400_000), now)).toBe("3d");
    expect(formatAge(ago(30 * 86_400_000), now)).toBe("4w");
    expect(formatAge(ago(800 * 86_400_000), now)).toBe("2y");
  });

  it("says nothing for a missing or unparseable instant", () => {
    expect(formatAge(null, now)).toBe("");
    expect(formatAge("", now)).toBe("");
    expect(formatAge("not a date", now)).toBe("");
  });

  it("reads a future instant as a countdown", () => {
    expect(formatAge(new Date(now + 2 * 86_400_000).toISOString(), now)).toBe(
      "in 2d",
    );
  });

  it("is never long enough to need truncating", () => {
    for (const ms of [0, 60_000, 3_600_000, 86_400_000, 1e11]) {
      expect(formatAge(ago(ms), now).length).toBeLessThanOrEqual(6);
    }
  });
});

describe("ageTitle", () => {
  it("carries the verb, the exact instant and the relative age", () => {
    const title = ageTitle(ago(8 * 3_600_000), "moved", now);
    expect(title.startsWith("Moved ")).toBe(true);
    expect(title).toContain("(8h)");
    // The exact instant, not a relative one.
    expect(title).toMatch(/\d{4}/);
  });

  it("drops the relative suffix when it says nothing", () => {
    expect(ageTitle(ago(1000), "checked", now)).not.toContain("(");
  });

  it("works without a verb", () => {
    const title = ageTitle(ago(86_400_000), "", now);
    expect(title).toContain("(1d)");
    expect(title.startsWith("Moved")).toBe(false);
  });

  it("says nothing for a missing instant", () => {
    expect(ageTitle(null, "moved", now)).toBe("");
    expect(ageTitle("not a date", "moved", now)).toBe("");
  });
});
