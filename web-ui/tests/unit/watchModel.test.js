import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  BRIEF_COMPUTED_FIELDS,
  freshnessLabel,
  initiativeLine,
  normalizeBrief,
  plainInline,
} from "../../src/lib/brief.js";

const NOW = Date.parse("2026-10-15T09:00:00Z");
const brief = normalizeBrief(
  JSON.parse(
    readFileSync(
      new URL(
        "../../../contracts/fixtures/brief/brief.v1.json",
        import.meta.url,
      ),
      "utf8",
    ),
  ),
);
const byId = (id) => brief.initiatives.find((row) => row.id === id);

describe("initiativeLine", () => {
  it("renders past promise from promise, past_promise_for and moves", () => {
    const line = initiativeLine(byId("battery-measurement"), NOW);
    expect(line.map((p) => p.text)).toEqual([
      "promised Oct 6",
      "9d past",
      `moved ${byId("battery-measurement").promiseMoves}×`,
    ]);
  });

  it("renders moving evidence age and the next promise", () => {
    const line = initiativeLine(byId("fleet-onboarding"), NOW);
    expect(line[0]).toEqual({
      field: "initiatives.last_evidence_at",
      text: "evidence 3h ago",
    });
    expect(line[1].text).toContain("next: enroll m6");
  });

  it("says last evidence for no promise, and that none exists", () => {
    expect(initiativeLine(byId("labels"), NOW)[0].text).toBe(
      "last evidence 12d ago",
    );
    expect(initiativeLine(byId("ble-reliability"), NOW)[0].text).toBe(
      "no evidence yet",
    );
  });

  it("names a known brief field on every part", () => {
    for (const row of brief.initiatives)
      for (const part of initiativeLine(row, NOW))
        expect(BRIEF_COMPUTED_FIELDS.has(part.field)).toBe(true);
  });
});

describe("freshnessLabel", () => {
  it("reads minutes, hours and nothing", () => {
    expect(freshnessLabel("2026-10-15T08:58:00Z", NOW)).toBe("as of 2m ago");
    expect(freshnessLabel("2026-10-15T06:00:00Z", NOW)).toBe("as of 3h ago");
    expect(freshnessLabel("2026-10-15T09:00:00Z", NOW)).toBe("as of just now");
    expect(freshnessLabel("", NOW)).toBe("");
  });
});

describe("plainInline", () => {
  it("strips inline markdown from an authored one-liner", () => {
    expect(plainInline("**fix** the top three")).toBe("fix the top three");
    expect(plainInline("land the `ask` fix, re-run the _whole_ sweep")).toBe(
      "land the ask fix, re-run the whole sweep",
    );
    expect(plainInline("see [notes](https://example.com)")).toBe("see notes");
    expect(plainInline("- [ ] todo")).toBe("todo");
  });

  it("leaves identifiers and arithmetic alone", () => {
    expect(plainInline("snake_case_name")).toBe("snake_case_name");
    expect(plainInline("2*3*4")).toBe("2*3*4");
  });

  it("feeds the computed line", () => {
    const row = {
      ...byId("observability"),
      promise: { at: "2026-10-28T00:00:00Z", what: "**dashboards** live" },
    };
    expect(initiativeLine(row, NOW).map((p) => p.text)).toContain(
      "next: dashboards live · Oct 28",
    );
  });
});
