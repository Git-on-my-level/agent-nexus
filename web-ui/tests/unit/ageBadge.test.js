import { describe, expect, it } from "vitest";

import { ageTitle, formatAge } from "../../src/lib/ageBadge.js";

const now = Date.parse("2026-10-04T12:00:00Z");
const ago = (ms) => new Date(now - ms).toISOString();

describe("formatAge", () => {
  it("uses the shared friendly phrases", () => {
    expect(formatAge(ago(30_000), now)).toBe("just now");
    expect(formatAge(ago(8 * 60_000), now)).toBe("8 min ago");
    const eightHours = ago(8 * 3_600_000);
    const here = new Date(now);
    const there = new Date(now - 8 * 3_600_000);
    const day =
      Date.UTC(here.getFullYear(), here.getMonth(), here.getDate()) -
      Date.UTC(there.getFullYear(), there.getMonth(), there.getDate());
    expect(formatAge(eightHours, now)).toBe(
      day === 86_400_000 ? "yesterday" : "8 h ago",
    );
    expect(formatAge(ago(3 * 86_400_000), now)).toMatch(
      /^[A-Z][a-z]{2} \d{1,2}/,
    );
    expect(formatAge(ago(400 * 86_400_000), now)).toMatch(/\d{4}/);
  });

  it("says nothing for a missing or unparseable instant", () => {
    expect(formatAge(null, now)).toBe("");
    expect(formatAge("", now)).toBe("");
    expect(formatAge("not a date", now)).toBe("");
  });

  it("reads a near future as a countdown and a later one as a date", () => {
    expect(formatAge(new Date(now + 10 * 60_000).toISOString(), now)).toBe(
      "in 10 min",
    );
    expect(
      formatAge(new Date(now + 2 * 86_400_000).toISOString(), now),
    ).toMatch(/^[A-Z][a-z]{2} \d{1,2}/);
  });
});

describe("ageTitle", () => {
  it("carries the verb and the exact local instant", () => {
    const title = ageTitle(ago(8 * 3_600_000), "moved", now);
    expect(title.startsWith("Moved ")).toBe(true);
    expect(title).toMatch(/\d{4}/);
    expect(title).not.toContain("(8h)");
  });

  it("works without a verb", () => {
    const title = ageTitle(ago(86_400_000), "", now);
    expect(title.startsWith("Moved")).toBe(false);
    expect(title).toMatch(/\d{4}/);
  });

  it("says nothing for a missing instant", () => {
    expect(ageTitle(null, "moved", now)).toBe("");
    expect(ageTitle("not a date", "moved", now)).toBe("");
  });
});
