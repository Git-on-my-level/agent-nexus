import { describe, expect, it } from "vitest";

import {
  buildThreadFilterQueryString,
  buildThreadFilterQueryParams,
  cadenceMatchesFilter,
  cadencePresetFromValue,
  cadenceToRequestValue,
  computeStaleness,
  formatCadenceLabel,
  readBackendStaleState,
  validateCadenceSelection,
  parseTagFilterInput,
} from "../../src/lib/threadFilters.js";

describe("thread filter query builders", () => {
  it("builds stable query string for selected filters", () => {
    const query = buildThreadFilterQueryString({
      status: "active",
      priority: "p1",
      cadence: "weekly",
      tags: ["ops", "customer"],
      staleness: "stale",
    });

    expect(query).toBe(
      "status=active&priority=p1&cadence=weekly&tag=ops&tag=customer&stale=true",
    );
  });

  it("builds request query object and parses tag input", () => {
    expect(parseTagFilterInput("ops, customer,,infra")).toEqual([
      "ops",
      "customer",
      "infra",
    ]);

    expect(
      buildThreadFilterQueryParams({
        status: "",
        priority: "p0",
        cadence: "",
        tags: ["ops"],
        staleness: "fresh",
      }),
    ).toEqual({
      priority: "p0",
      tag: ["ops"],
      stale: false,
    });
  });

  it("preserves multiple tags in request query (match-all semantics)", () => {
    expect(
      buildThreadFilterQueryParams({
        tags: ["ops", "customer"],
      }),
    ).toEqual({
      tag: ["ops", "customer"],
    });
  });

  it("maps presets to reactive-or-cron request cadence values", () => {
    expect(cadenceToRequestValue({ preset: "reactive" })).toBe("reactive");
    expect(cadenceToRequestValue({ preset: "daily" })).toBe("0 9 * * *");
    expect(cadenceToRequestValue({ preset: "weekly" })).toBe("0 9 * * 1");
    expect(cadenceToRequestValue({ preset: "monthly" })).toBe("0 9 1 * *");
    expect(
      cadenceToRequestValue({
        preset: "custom",
        customCron: "*/15 * * * *",
      }),
    ).toBe("*/15 * * * *");
  });

  it("infers cadence preset from legacy and cron values", () => {
    expect(cadencePresetFromValue("reactive")).toBe("reactive");
    expect(cadencePresetFromValue("daily")).toBe("daily");
    expect(cadencePresetFromValue("0 9 * * *")).toBe("daily");
    expect(cadencePresetFromValue("0 9 * * 1")).toBe("weekly");
    expect(cadencePresetFromValue("*/15 * * * *")).toBe("custom");
    expect(cadencePresetFromValue("custom")).toBe("custom");
  });

  it("validates custom cadence input and supports legacy custom fallback", () => {
    expect(
      validateCadenceSelection({
        preset: "custom",
        customCron: "*/10 * * * *",
      }),
    ).toBe("");
    expect(
      validateCadenceSelection({
        preset: "custom",
        customCron: "invalid cron",
      }),
    ).toBe("Custom schedule must be a 5-field cron expression.");
    expect(
      validateCadenceSelection({
        preset: "custom",
        customCron: "",
        fallbackCadence: "custom",
        allowLegacyCustom: true,
      }),
    ).toBe("");
  });

  it("matches cadence filters by preset across legacy and cron values", () => {
    expect(cadenceMatchesFilter("daily", "daily")).toBe(true);
    expect(cadenceMatchesFilter("0 9 * * *", "daily")).toBe(true);
    expect(cadenceMatchesFilter("0 9 * * 1", "daily")).toBe(false);
    expect(cadenceMatchesFilter("*/15 * * * *", "custom")).toBe(true);
    expect(cadenceMatchesFilter("custom", "custom")).toBe(true);
    expect(cadenceMatchesFilter("reactive", "custom")).toBe(false);
  });

  it("formats cadence labels for UI display", () => {
    expect(formatCadenceLabel("reactive")).toBe("Reactive");
    expect(formatCadenceLabel("0 9 * * *")).toBe("Daily");
    expect(formatCadenceLabel("*/15 * * * *")).toBe("Custom (*/15 * * * *)");
    expect(formatCadenceLabel("custom")).toBe("Custom");
    expect(
      formatCadenceLabel("*/15 * * * *", { includeExpression: false }),
    ).toBe("Custom");
  });

  it("prefers backend stale state when present", () => {
    expect(computeStaleness({ stale: true }).stale).toBe(true);
    expect(computeStaleness({ stale: true }).label).toBe("Stale");

    expect(computeStaleness({ stale: false }).stale).toBe(false);
    expect(computeStaleness({ stale: false }).label).toBe("Fresh");
  });

  it("reads backend stale state across supported flag names", () => {
    expect(readBackendStaleState({ stale: true })).toBe(true);
    expect(readBackendStaleState({ stale: false })).toBe(false);
    expect(readBackendStaleState({})).toBeNull();
    expect(readBackendStaleState(null)).toBeNull();
  });

  it("falls back to local check-in heuristics when stale state is absent", () => {
    expect(computeStaleness({ next_check_in_at: null }).label).toBe(
      "No check-in",
    );
    expect(
      computeStaleness({
        next_check_in_at: new Date(Date.now() - 60_000).toISOString(),
      }).stale,
    ).toBe(true);
  });
});
