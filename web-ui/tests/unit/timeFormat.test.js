import { describe, expect, it, vi } from "vitest";

import { retainClock, TIME_TICK_MS } from "../../src/lib/time/clock.svelte.js";
import {
  formatAgeSeconds,
  formatElapsed,
  formatTime,
  instantIso,
} from "../../src/lib/time/format.js";

const NOW = Date.parse("2026-10-04T12:00:00Z");
const at = (ms) => new Date(NOW + ms).toISOString();

describe("formatTime", () => {
  it("uses the documented thresholds", () => {
    const opts = { now: NOW, locale: "en-US" };
    expect(formatTime(at(-20_000), opts)).toBe("just now");
    expect(formatTime(at(20_000), opts)).toBe("just now");
    expect(formatTime(at(-15 * 60_000), opts)).toBe("15 min ago");
    expect(formatTime(at(10 * 60_000), opts)).toBe("in 10 min");
    expect(formatTime(at(-3 * 3_600_000), opts)).toBe("3 h ago");
    expect(formatTime(at(2 * 3_600_000), opts)).toBe("in 2 h");
    const dayDelta = (ms) => {
      const here = new Date(NOW);
      const there = new Date(NOW + ms);
      return Math.round(
        (Date.UTC(here.getFullYear(), here.getMonth(), here.getDate()) -
          Date.UTC(there.getFullYear(), there.getMonth(), there.getDate())) /
          86_400_000,
      );
    };
    expect(formatTime(at(-20 * 3_600_000), opts)).toBe(
      dayDelta(-20 * 3_600_000) === 1 ? "yesterday" : "20 h ago",
    );
    expect(formatTime(at(20 * 3_600_000), opts)).toBe(
      dayDelta(20 * 3_600_000) === -1 ? "tomorrow" : "in 20 h",
    );
    const older = new Date(NOW - 10 * 86_400_000);
    const sameYear = older.getFullYear() === new Date(NOW).getFullYear();
    expect(formatTime(at(-10 * 86_400_000), opts)).toBe(
      new Intl.DateTimeFormat("en-US", {
        month: "short",
        day: "numeric",
        ...(sameYear ? {} : { year: "numeric" }),
      }).format(older),
    );
    expect(formatTime("2025-10-05T12:00:00Z", opts)).toMatch(/, 2025$/);
  });

  it("puts the timezone abbreviation on the exact form", () => {
    const exact = formatTime(NOW, {
      now: NOW,
      locale: "en-US",
      style: "exact",
    });
    const zone = new Intl.DateTimeFormat("en-US", { timeZoneName: "short" })
      .formatToParts(new Date(NOW))
      .find((part) => part.type === "timeZoneName")?.value;
    expect(zone).toBeTruthy();
    expect(zone).not.toMatch(/^(AM|PM)$/);
    expect(exact).toContain(zone);
  });

  it("returns empty for an unparseable value", () => {
    expect(formatTime("")).toBe("");
    expect(formatTime(null)).toBe("");
    expect(formatTime("not-a-date")).toBe("");
    expect(formatTime(Number.NaN)).toBe("");
    expect(formatTime(new Date(Number.NaN))).toBe("");
    expect(formatTime("not-a-date", { style: "exact" })).toBe("");
    expect(instantIso(null)).toBe("");
    expect(instantIso("")).toBe("");
  });

  it("formats a duration separately from an instant", () => {
    expect(formatElapsed(41 * 60_000)).toBe("41m");
    expect(formatElapsed(20_000)).toBe("<1m");
    expect(formatAgeSeconds(-120, NOW)).toBe("just now");
  });
});

describe("shared clock", () => {
  it("opens one interval for every subscriber", () => {
    vi.useFakeTimers();
    const previous = globalThis.window;
    globalThis.window = {
      setInterval: (...args) => setInterval(...args),
      clearInterval: (...args) => clearInterval(...args),
    };
    const releaseA = retainClock();
    const releaseB = retainClock();
    expect(vi.getTimerCount()).toBe(1);
    releaseA();
    expect(vi.getTimerCount()).toBe(1);
    releaseB();
    expect(vi.getTimerCount()).toBe(0);
    globalThis.window = previous;
    vi.useRealTimers();
  });

  it("ticks on a low frequency", () => {
    expect(TIME_TICK_MS).toBeGreaterThanOrEqual(15_000);
    expect(TIME_TICK_MS).toBeLessThanOrEqual(60_000);
  });
});
