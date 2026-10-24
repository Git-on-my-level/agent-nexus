import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  briefLoops,
  findBriefRow,
  isLoop,
  normalizeBrief,
} from "../../src/lib/brief.js";
import { collectionFromLoops } from "../../src/lib/collection.js";

/*
 * Core puts loop-bound cards in `brief.loops` and never in `initiatives`
 * (see docs/grouping-work.md). The shared fixture has that shape.
 */
const raw = JSON.parse(
  readFileSync(
    new URL("../../../contracts/fixtures/brief/brief.v1.json", import.meta.url),
    "utf8",
  ),
);
const NOW = Date.parse("2026-10-15T09:00:00Z");

describe("loops live in brief.loops", () => {
  it("the fixture is shaped like core: no loop state inside initiatives", () => {
    expect(raw.initiatives.filter((row) => row.loop)).toEqual([]);
    expect(raw.loops.map((row) => row.ref)).toEqual(["card:signup-loop"]);
  });

  it("normalizeBrief keeps the loops section", () => {
    const brief = normalizeBrief(raw);
    expect(brief.loops.map((row) => row.ref)).toEqual(["card:signup-loop"]);
    expect(brief.loops[0].loop?.condition).toBeTruthy();
    expect(brief.truncated.loops).toBe(false);
  });

  it("a row core sent as a loop is a loop before its first receipt", () => {
    const brief = normalizeBrief({
      loops: [{ ref: "card:new-loop", title: "New loop", loop: null }],
    });
    expect(isLoop(brief.loops[0])).toBe(true);
  });

  it("Watch's Loops view lists brief.loops, then measured initiatives", () => {
    const brief = normalizeBrief(raw);
    expect(briefLoops(brief).map((row) => row.ref)).toEqual([
      "card:signup-loop",
      "card:battery-measurement",
    ]);
    const refs = collectionFromLoops(briefLoops(brief), { now: NOW })
      .groups.flatMap((group) => group.rows)
      .map((row) => row.ref);
    expect(refs).toContain("card:signup-loop");
  });

  it("never lists a card twice", () => {
    const brief = normalizeBrief({
      initiatives: [{ ref: "card:x", title: "X", series: [{ ref: "s" }] }],
      loops: [{ ref: "card:x", title: "X" }],
    });
    expect(briefLoops(brief).map((row) => row.section)).toEqual(["loops"]);
  });

  it("finds a card's row in either section", () => {
    const brief = normalizeBrief(raw);
    expect(findBriefRow(brief, "card:signup-loop")?.title).toBe(
      "Signup conversion loop",
    );
    expect(findBriefRow(brief, "card:observability")?.title).toBe(
      "Observability",
    );
    expect(findBriefRow(brief, "card:missing")).toBeNull();
  });
});
