import { describe, expect, it } from "vitest";
import {
  formatMetricValue,
  groupLoops,
  isLoop,
  loopGroup,
  LOOP_SILENT_SECONDS,
} from "../../src/lib/brief.js";

const NOW = Date.parse("2026-10-15T12:00:00Z");
const ago = (seconds) => new Date(NOW - seconds * 1000).toISOString();
const SERIES = [{ ref: "series:conversion", name: "conversion" }];

function loop(overrides = {}) {
  return {
    ref: `card:${overrides.id ?? "x"}`,
    id: overrides.id ?? "x",
    title: overrides.title ?? overrides.id ?? "x",
    state: "moving",
    placement: "in_progress",
    promise: null,
    pastPromiseFor: 0,
    lastEvidenceAt: ago(3600),
    waitingOnYouCount: 0,
    series: SERIES,
    metricFlat: null,
    ...overrides,
  };
}

describe("Watch loops", () => {
  it("counts only initiatives with a series", () => {
    expect(isLoop(loop())).toBe(true);
    expect(isLoop(loop({ series: [] }))).toBe(false);
    expect(isLoop({})).toBe(false);
  });

  it("groups by what the loop needs, first match wins", () => {
    expect(
      loopGroup(loop({ waitingOnYouCount: 1, state: "past_promise" }), NOW),
    ).toBe("waiting");
    expect(loopGroup(loop({ state: "past_promise" }), NOW)).toBe("silent");
    // No evidence yet (or an unreadable date) is not silence.
    expect(loopGroup(loop({ lastEvidenceAt: "" }), NOW)).toBe("moving");
    expect(loopGroup(loop({ lastEvidenceAt: null }), NOW)).toBe("moving");
    expect(loopGroup(loop({ lastEvidenceAt: "not a date" }), NOW)).toBe(
      "moving",
    );
    expect(
      loopGroup(loop({ lastEvidenceAt: ago(LOOP_SILENT_SECONDS - 1) }), NOW),
    ).toBe("moving");
    expect(
      loopGroup(loop({ lastEvidenceAt: ago(LOOP_SILENT_SECONDS) }), NOW),
    ).toBe("silent");
    expect(
      loopGroup(
        loop({
          metricFlat: {
            flat: true,
            for_seconds: 4 * 86400,
            window_seconds: 604800,
          },
        }),
        NOW,
      ),
    ).toBe("flat");
    expect(
      loopGroup(
        loop({
          metricFlat: { flat: false, for_seconds: 0, window_seconds: 604800 },
        }),
        NOW,
      ),
    ).toBe("moving");
    expect(loopGroup(loop({ state: "done", waitingOnYouCount: 2 }), NOW)).toBe(
      "done",
    );
  });

  it("orders groups by attention and rows within a group", () => {
    const groups = groupLoops(
      [
        loop({ id: "moving" }),
        loop({ id: "quiet-4d", lastEvidenceAt: ago(4 * 86400) }),
        loop({ id: "quiet-9d", lastEvidenceAt: ago(9 * 86400) }),
        loop({
          id: "flat-2d",
          metricFlat: {
            flat: true,
            for_seconds: 2 * 86400,
            window_seconds: 604800,
          },
        }),
        loop({
          id: "flat-6d",
          metricFlat: {
            flat: true,
            for_seconds: 6 * 86400,
            window_seconds: 604800,
          },
        }),
        loop({ id: "ask", waitingOnYouCount: 1 }),
        loop({ id: "not-a-loop", series: [] }),
      ],
      NOW,
    );
    expect(groups.map((g) => g.group)).toEqual([
      "waiting",
      "silent",
      "flat",
      "moving",
    ]);
    expect(groups[1].rows.map((r) => r.id)).toEqual(["quiet-9d", "quiet-4d"]);
    expect(groups[2].rows.map((r) => r.id)).toEqual(["flat-6d", "flat-2d"]);
    expect(
      groups.flatMap((g) => g.rows).some((r) => r.id === "not-a-loop"),
    ).toBe(false);
  });

  it("puts a never-evidenced loop past its promise after dated silence", () => {
    const [silent] = groupLoops(
      [
        loop({ id: "new", lastEvidenceAt: null, state: "past_promise" }),
        loop({ id: "quiet-9d", lastEvidenceAt: ago(9 * 86400) }),
      ],
      NOW,
    );
    expect(silent.rows.map((r) => r.id)).toEqual(["quiet-9d", "new"]);
  });

  it("breaks ties by the initiative order and keeps Done last", () => {
    const groups = groupLoops(
      [
        loop({ id: "done", state: "done" }),
        loop({ id: "b", title: "b", waitingOnYouCount: 1 }),
        loop({ id: "a", title: "a", waitingOnYouCount: 1 }),
      ],
      NOW,
    );
    expect(groups.map((g) => g.group)).toEqual(["waiting", "done"]);
    expect(groups[0].rows).toHaveLength(2);
    expect(new Set(groups[0].rows.map((r) => r.id))).toEqual(
      new Set(["a", "b"]),
    );
  });

  it("formats metric values without noise", () => {
    expect(formatMetricValue(null)).toBe("");
    expect(formatMetricValue("")).toBe("");
    expect(formatMetricValue(0)).toBe("0");
    expect(formatMetricValue(0.123456)).toBe("0.123");
    expect(formatMetricValue(42.345)).toBe("42.3");
    expect(formatMetricValue(12345.6)).toBe("12,346");
    expect(formatMetricValue("ok")).toBe("ok");
    expect(formatMetricValue("3.5")).toBe("3.5");
    expect(formatMetricValue(-12345.6)).toBe("-12,346");
    expect(formatMetricValue(-0.0001)).toBe("-1.00e-4");
    expect(formatMetricValue(NaN)).toBe("");
    expect(formatMetricValue(Infinity)).toBe("");
    expect(formatMetricValue(Number.MAX_VALUE)).toBe("1.80e+308");
    expect(formatMetricValue("1e2")).toBe("1e2");
    expect(formatMetricValue("NaN")).toBe("NaN");
  });
});
