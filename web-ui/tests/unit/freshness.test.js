import { describe, expect, it } from "vitest";

import {
  FRESHNESS_EXPECTATION_HOURS,
  expectationHoursFor,
  formatExpectation,
  freshnessKindForPhase,
  freshnessModel,
} from "../../src/lib/freshness.js";

const NOW = Date.parse("2026-10-07T12:00:00Z");
const ago = (hours) => new Date(NOW - hours * 3_600_000).toISOString();

describe("freshnessKindForPhase", () => {
  it("maps a phase to the cadence its work implies", () => {
    expect(freshnessKindForPhase("in_progress")).toBe("in_progress");
    expect(freshnessKindForPhase("backlog")).toBe("backlog");
    expect(freshnessKindForPhase("blocked")).toBe("waiting");
    expect(freshnessKindForPhase("review")).toBe("waiting");
    expect(freshnessKindForPhase("ready")).toBe("waiting");
    expect(freshnessKindForPhase("done")).toBe("closed");
    expect(freshnessKindForPhase("cancelled")).toBe("closed");
  });

  it("gives an unnamed phase the middle expectation, not an exemption", () => {
    expect(freshnessKindForPhase("vendor_waiting")).toBe("waiting");
    expect(freshnessKindForPhase("")).toBe("waiting");
  });

  it("reads an archived or trashed resource as finished", () => {
    expect(freshnessKindForPhase("in_progress", "archived")).toBe("closed");
    expect(freshnessKindForPhase("in_progress", "trashed")).toBe("closed");
  });
});

describe("expectations", () => {
  it("defaults by kind: a day in progress, three days waiting, a fortnight parked", () => {
    expect(FRESHNESS_EXPECTATION_HOURS.in_progress).toBe(24);
    expect(FRESHNESS_EXPECTATION_HOURS.initiative).toBe(72);
    expect(FRESHNESS_EXPECTATION_HOURS.waiting).toBe(72);
    expect(FRESHNESS_EXPECTATION_HOURS.backlog).toBe(336);
    expect(FRESHNESS_EXPECTATION_HOURS.closed).toBeNull();
  });

  it("prefers an explicit override, then the row's own field", () => {
    expect(expectationHoursFor("in_progress")).toBe(24);
    expect(
      expectationHoursFor("in_progress", {
        row: { update_expectation_hours: 6 },
      }),
    ).toBe(6);
    expect(
      expectationHoursFor("in_progress", {
        override: 2,
        row: { update_expectation_hours: 6 },
      }),
    ).toBe(2);
  });

  it("ignores a junk override rather than badging against nonsense", () => {
    for (const bad of [0, -3, "soon", null, NaN]) {
      expect(expectationHoursFor("in_progress", { override: bad })).toBe(24);
    }
  });

  it("writes an expectation the way a tooltip reads it", () => {
    expect(formatExpectation(24)).toBe("1d");
    expect(formatExpectation(72)).toBe("3d");
    expect(formatExpectation(336)).toBe("14d");
    expect(formatExpectation(12)).toBe("12h");
    expect(formatExpectation(0)).toBe("");
  });
});

describe("freshnessModel", () => {
  it("is green inside the expectation, amber up to twice it, red beyond", () => {
    const at = (hours) =>
      freshnessModel(ago(hours), { kind: "in_progress", now: NOW });
    expect(at(2).tone).toBe("ok");
    expect(at(23).tone).toBe("ok");
    expect(at(25).tone).toBe("warn");
    expect(at(47).tone).toBe("warn");
    expect(at(49).tone).toBe("danger");
  });

  it("judges the same age differently by kind", () => {
    const threeDays = ago(72);
    expect(freshnessModel(threeDays, { kind: "backlog", now: NOW }).tone).toBe(
      "ok",
    );
    expect(
      freshnessModel(threeDays, { kind: "initiative", now: NOW }).tone,
    ).toBe("ok");
    expect(
      freshnessModel(threeDays, { kind: "in_progress", now: NOW }).tone,
    ).toBe("danger");
  });

  it("shows the relative time, the way the age badge writes it", () => {
    expect(
      freshnessModel(ago(0.2), { kind: "in_progress", now: NOW }).age,
    ).toBe("12m");
    expect(freshnessModel(ago(72), { kind: "backlog", now: NOW }).age).toBe(
      "3d",
    );
  });

  it("carries the expectation and the exact instant in the tooltip", () => {
    const fresh = freshnessModel(ago(2), {
      kind: "in_progress",
      verb: "moved",
      now: NOW,
    });
    expect(fresh.title).toMatch(/^Moved /);
    expect(fresh.title).toContain("(2h)");
    expect(fresh.title).toContain("within the expected 1d");

    const late = freshnessModel(ago(100), {
      kind: "in_progress",
      verb: "moved",
      now: NOW,
    });
    expect(late.title).toContain("expected every 1d");
    expect(late.state).toBe("very_late");
  });

  it("shows no badge at all for finished or archived work", () => {
    expect(freshnessModel(ago(400), { kind: "closed", now: NOW })).toBeNull();
  });

  it("shows no badge without a usable instant", () => {
    for (const bad of [null, "", "not a date", undefined]) {
      expect(freshnessModel(bad, { kind: "in_progress", now: NOW })).toBeNull();
    }
  });

  it("reads a future instant as fresh rather than overdue", () => {
    const ahead = new Date(NOW + 2 * 3_600_000).toISOString();
    const model = freshnessModel(ahead, { kind: "in_progress", now: NOW });
    expect(model.tone).toBe("ok");
    expect(model.age).toBe("in 2h");
  });
});
