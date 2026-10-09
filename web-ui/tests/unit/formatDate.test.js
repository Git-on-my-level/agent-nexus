import { afterEach, describe, expect, it, vi } from "vitest";

import {
  datetimeLocalToIso,
  formatAbsoluteDateTime,
  formatTimestamp,
  isoToDatetimeLocal,
} from "../../src/lib/formatDate.js";

describe("formatDate", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("formats recent past and future timestamps relatively", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-03-15T12:00:00.000Z"));

    expect(formatTimestamp("2026-03-15T11:59:45.000Z")).toBe("just now");
    expect(formatTimestamp("2026-03-15T12:00:15.000Z")).toBe("just now");
    expect(formatTimestamp("2026-03-15T11:55:00.000Z")).toBe("5 min ago");
    expect(formatTimestamp("2026-03-15T14:00:00.000Z")).toBe("in 2 h");
    expect(formatTimestamp("2026-03-13T12:00:00.000Z")).toMatch(
      /^[A-Z][a-z]{2} \d{1,2}/,
    );
  });

  it("falls back to an absolute date for older timestamps", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-03-15T12:00:00.000Z"));

    expect(formatTimestamp("2026-03-01T09:30:00.000Z")).toMatch(
      /^[A-Z][a-z]{2} \d{1,2}/,
    );
  });

  it("formats absolute local date and time for triage surfaces", () => {
    const s = formatAbsoluteDateTime("2026-03-15T14:30:00.000Z");
    expect(s.length).toBeGreaterThan(8);
    expect(s).toMatch(/2026/);
    expect(formatAbsoluteDateTime("")).toBe("");
  });

  it("handles missing and invalid inputs", () => {
    expect(formatTimestamp("")).toBe("");
    expect(formatTimestamp(null)).toBe("");
    expect(formatTimestamp("not-a-date")).toBe("");
    expect(isoToDatetimeLocal("")).toBe("");
    expect(isoToDatetimeLocal("not-a-date")).toBe("");
    expect(datetimeLocalToIso("")).toBe("");
    expect(datetimeLocalToIso("not-a-date")).toBe("");
  });

  it("round-trips datetime-local values at minute precision", () => {
    const localValue = "2026-03-15T12:34";

    expect(isoToDatetimeLocal(datetimeLocalToIso(localValue))).toBe(localValue);
  });
});
