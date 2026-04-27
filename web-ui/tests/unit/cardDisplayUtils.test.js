import { describe, expect, it } from "vitest";

import { dueDateDisplay, isOverdue } from "../../src/lib/cardDisplayUtils.js";

describe("cardDisplayUtils", () => {
  it("formats due dates and ignores empty values", () => {
    expect(dueDateDisplay("")).toBe("");
    expect(dueDateDisplay("not-a-date")).toBe("");
    expect(dueDateDisplay("2026-03-05T00:00:00.000Z")).toContain("2026");
  });

  it("detects overdue due dates", () => {
    expect(isOverdue("")).toBe(false);
    expect(isOverdue("2999-01-01T00:00:00.000Z")).toBe(false);
    expect(isOverdue("2000-01-01T00:00:00.000Z")).toBe(true);
  });
});
