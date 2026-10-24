import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { BRIEF_COMPUTED_FIELDS, normalizeBrief } from "../../src/lib/brief.js";
import {
  activityByDay,
  attentionCount,
  boardColumns,
  collectionFromInitiatives,
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

describe("collectionFromInitiatives", () => {
  const collection = collectionFromInitiatives(brief.initiatives, {
    now: NOW,
    href: (row) => `/i/${row.id}`,
    decideHref: (row) => `/decide?initiative=${row.ref}`,
  });

  it("puts a decision for the reader ahead of the computed state", () => {
    const battery = rows(collection).find(
      (r) => r.ref === "card:battery-measurement",
    );
    expect(battery.state).toBe("waiting_on_you");
    expect(battery.computedState).toBe("past_promise");
    expect(battery.next).toMatchObject({ label: "You", isViewer: true });
    expect(battery.time).toMatchObject({
      tone: "warning",
      field: "initiatives.past_promise_for",
    });
  });

  it("tags every computed piece with a brief field", () => {
    for (const row of rows(collection)) {
      expect(BRIEF_COMPUTED_FIELDS.has(row.stateField)).toBe(true);
      for (const part of row.subtitle?.parts ?? [])
        expect(BRIEF_COMPUTED_FIELDS.has(part.field)).toBe(true);
      if (row.time)
        expect(BRIEF_COMPUTED_FIELDS.has(row.time.field)).toBe(true);
    }
  });

  it("counts attention rows for the tab badge", () => {
    const waiting = brief.initiatives.filter(
      (r) => r.waitingOnYouCount > 0 && r.state !== "done",
    ).length;
    expect(attentionCount(collection)).toBe(waiting);
  });

  it("filters and buckets rows by state", () => {
    const moving = filterRows(collection, "moving").flatMap((g) => g.rows);
    expect(moving.every((r) => r.state === "moving")).toBe(true);
    const board = boardColumns(collection);
    expect(board.map((c) => c.key)).toEqual(
      collection.states.map((s) => s.key).filter((k) => collection.counts[k]),
    );
  });

  it("does not draw columns that no row fills", () => {
    expect(columnsFor(collection)).toMatchObject({
      owner: false,
      activity: false,
      progress: true,
    });
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
});
