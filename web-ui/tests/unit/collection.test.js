import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { BRIEF_COMPUTED_FIELDS, normalizeBrief } from "../../src/lib/brief.js";
import {
  activityByDay,
  attentionCount,
  boardColumns,
  collectionFromLoops,
  columnsFor,
  filterRows,
  normalizeCollection,
  sparkPoints,
} from "../../src/lib/collection.js";
import { validateLiveQuery } from "../../src/lib/liveReports.js";

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
const rows = (collection) => collection.groups.flatMap((g) => g.rows);

describe("collection helpers on the Loops collection", () => {
  const collection = collectionFromLoops(brief.initiatives, { now: NOW });

  it("tags every computed piece with a brief field", () => {
    for (const row of rows(collection)) {
      if (row.stateField)
        expect(BRIEF_COMPUTED_FIELDS.has(row.stateField)).toBe(true);
      if (row.time?.field)
        expect(BRIEF_COMPUTED_FIELDS.has(row.time.field)).toBe(true);
    }
  });

  it("counts attention rows for the tab badge", () => {
    const hot = new Set(
      collection.states.filter((s) => s.tone === "attention").map((s) => s.key),
    );
    expect(attentionCount(collection)).toBe(
      rows(collection).filter((r) => hot.has(r.state)).length,
    );
  });

  it("filters and buckets rows by state", () => {
    const [first] = rows(collection);
    const picked = filterRows(collection, first.state).flatMap((g) => g.rows);
    expect(picked.every((r) => r.state === first.state)).toBe(true);
    const board = boardColumns(collection);
    expect(board.map((c) => c.key)).toEqual(
      collection.states.map((s) => s.key).filter((k) => collection.counts[k]),
    );
  });

  it("does not draw columns that no row fills", () => {
    expect(columnsFor(collection)).toMatchObject({ owner: false });
  });
});

describe("collectionFromLoops", () => {
  it("lists only loops, grouped by what they need", () => {
    const collection = collectionFromLoops(brief.initiatives, { now: NOW });
    const refs = rows(collection).map((r) => r.ref);
    expect(refs).toContain("card:battery-measurement");
    expect(refs).not.toContain("card:observability");
  });
  it("names the measure with the goal's label, else its metric key", () => {
    const base = brief.initiatives.find(
      (r) => r.ref === "card:battery-measurement",
    );
    const withGoal = (label) => ({
      ...base,
      loop: {
        ...base.loop,
        goal: {
          metric: "render_ms",
          direction: "down",
          unit: "ms",
          ...(label ? { label } : {}),
          points: [
            { t: "2026-10-01T00:00:00Z", value: 300 },
            { t: "2026-10-02T00:00:00Z", value: 120 },
          ],
        },
      },
    });
    const progressOf = (row) =>
      rows(collectionFromLoops([row], { now: NOW })).find(
        (r) => r.ref === base.ref,
      ).progress;
    expect(progressOf(withGoal("Cold render time")).metric).toBe(
      "Cold render time",
    );
    expect(progressOf(withGoal("")).metric).toBe("render_ms");
  });
});

describe("normalizeCollection", () => {
  it("keeps unknown states visible as neutral and reads every progress kind", () => {
    const collection = normalizeCollection({
      states: [{ key: "working", label: "Working", tone: "good" }],
      groups: [
        {
          key: "all",
          rows: [
            {
              ref: "agent:hermes",
              title: "hermes",
              state: "working",
              progress: { kind: "label", label: "Smart-merge fix" },
              activity: [0, 1, 2],
              next_actor: { kind: "agent", label: "hermes", is_viewer: false },
            },
            {
              ref: "card:x",
              title: "X",
              state: "brand_new_state",
              progress: {
                kind: "segments",
                segments: [{ status: "done" }, { status: "weird" }],
                label: "1 of 2",
              },
            },
            {
              ref: "card:y",
              title: "Y",
              state: "working",
              progress: {
                kind: "spark",
                points: [
                  { t: "2026-10-01T00:00:00Z", v: 1 },
                  { t: "2026-10-02T00:00:00Z", v: 2 },
                ],
                label: "2",
              },
              changed_since_last_visit: true,
            },
          ],
        },
      ],
    });
    expect(collection.states.map((s) => s.key)).toEqual([
      "working",
      "brand_new_state",
    ]);
    expect(collection.states[1].tone).toBe("neutral");
    const [a, b, c] = rows(collection);
    expect(a.progress).toEqual({ kind: "label", label: "Smart-merge fix" });
    expect(b.progress.segments.map((s) => s.status)).toEqual(["done", "todo"]);
    expect(c.progress.values).toEqual([1, 2]);
    expect(c.changed).toBe(true);
    expect(collection.counts).toEqual({ working: 2, brand_new_state: 1 });
  });
});

describe("helpers", () => {
  it("buckets timestamps into UTC days, oldest first", () => {
    const counts = activityByDay(
      [
        "2026-10-15T01:00:00Z",
        "2026-10-15T08:00:00Z",
        "2026-10-14T23:00:00Z",
        "2026-09-01T00:00:00Z",
        "nope",
      ],
      NOW,
      3,
    );
    expect(counts).toEqual([0, 1, 2]);
  });

  it("draws a flat sparkline in the middle and needs two points", () => {
    expect(sparkPoints([5])).toBe("");
    expect(sparkPoints([5, 5], 10, 10, 0)).toBe("0.0,5.0 10.0,5.0");
  });
});

describe("live-collection query validation (mirrors core)", () => {
  const ok = (type, data) => expect(validateLiveQuery(type, data)).toEqual([]);
  const bad = (type, data) =>
    expect(validateLiveQuery(type, data).length).toBeGreaterThan(0);
  it("accepts each collection with its own states", () => {
    ok("live-collection", { collection: "initiatives", group_by: "project" });
    ok("live-collection", {
      collection: "loops",
      status: ["failing", "stopped_reporting"],
      group_by: "none",
      limit: 48,
    });
    ok("live-collection", { collection: "agents", status: ["working"] });
  });
  it("rejects what core rejects", () => {
    bad("live-collection", { collection: "cards" });
    bad("live-collection", { collection: "loops", status: ["working"] });
    bad("live-collection", { collection: "loops", status: ["flat", "flat"] });
    bad("live-collection", { collection: "loops", group_by: "phase" });
    bad("live-collection", { collection: "loops", limit: 49 });
    bad("live-collection", { collection: "loops", sort: "title" });
  });
  it("scopes activity to subjects or a collection", () => {
    ok("live-activity", { collection: "loops", subject_refs: ["card:a"] });
    bad("live-activity", { collection: "agents" });
    bad("live-activity", { subject_refs: ["card:a", "card:a"] });
    bad("live-activity", { subject_refs: ["no-colon"] });
  });
  it("matches core on an empty collection and byte-long subject refs", () => {
    // Core's zero value: an empty collection means "no collection scope".
    ok("live-activity", { collection: "" });
    // 1,500 two-byte characters are 3,000 bytes, which core counts.
    bad("live-activity", { subject_refs: [`card:${"é".repeat(1500)}`] });
    ok("live-activity", { subject_refs: [`card:${"a".repeat(2043)}`] });
  });
});
