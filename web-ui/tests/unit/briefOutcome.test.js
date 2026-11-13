import { describe, expect, it } from "vitest";
import { BRIEF_COMPUTED_FIELDS, normalizeBrief } from "../../src/lib/brief.js";
import {
  demotePromiseParts,
  formatMeasureValue,
  formatNumber,
  fyiRepeatText,
  measureText,
  measureView,
  normalizeMeasure,
  sparklinePoints,
  upkeepNotice,
} from "../../src/lib/briefOutcome.js";
import fixture from "../../../contracts/fixtures/brief/brief.v1.json";

const NOW = Date.parse("2026-10-15T09:00:00Z");
const brief = normalizeBrief(fixture);
const row = (ref) => brief.initiatives.find((item) => item.ref === ref);

describe("measure", () => {
  it("reads the fixture's reading, change and target", () => {
    const measure = row("card:battery-measurement").measure;
    expect(measureView(measure, NOW)).toEqual({
      label: "Battery life per charge",
      reading: "41.5h → 48h",
      target: "target 48h",
      change: { text: "up 2.3 in 7d", tone: "ok" },
      stale: "read 1d ago",
      onTarget: false,
    });
    expect(measureText(measure, NOW)).toBe(
      "Battery life per charge 41.5h → 48h · up 2.3 in 7d · read 1d ago",
    );
  });

  it("says No reading, with the target, when nothing publishes the number", () => {
    const measure = row("card:fleet-onboarding").measure;
    expect(measureView(measure, NOW)).toMatchObject({
      reading: null,
      target: "target 7",
      change: null,
    });
    expect(measureText(measure, NOW)).toBe(
      "Hosts enrolled: No reading · target 7",
    );
  });

  it("tones the change by the wanted direction", () => {
    const base = {
      label: "Monthly churn",
      direction: "down",
      target: 10,
      unit: "%",
      latest_at: "2026-10-14T00:00:00Z",
      previous_at: "2026-10-07T00:00:00Z",
    };
    const view = (latest, previous) =>
      measureView(normalizeMeasure({ ...base, latest, previous }), NOW);
    expect(view(12.4, 13.1).change).toEqual({
      text: "down 0.7 in 7d",
      tone: "ok",
    });
    expect(view(13.1, 12.4).change).toEqual({
      text: "up 0.7 in 7d",
      tone: "danger",
    });
    expect(view(12.4, 12.4).change).toEqual({
      text: "flat in 7d",
      tone: "muted",
    });
    expect(view(12.4, 13.1).reading).toBe("12.4% → 10%");
  });

  it("shows how old a reading is", () => {
    const measure = normalizeMeasure({
      label: "Margin",
      direction: "up",
      target: 50,
      unit: "%",
      latest: 44,
      latest_at: "2026-10-11T09:00:00Z",
      age_seconds: 4 * 86_400,
    });
    expect(measureView(measure, NOW).stale).toBe("read 4d ago");
  });

  it("formats numbers and units without noise", () => {
    expect(formatNumber(41.5)).toBe("41.5");
    expect(formatNumber(7)).toBe("7");
    expect(formatNumber(12.345)).toBe("12.35");
    expect(formatMeasureValue(12.4, "%")).toBe("12.4%");
    expect(formatMeasureValue(3, "hosts")).toBe("3 hosts");
    expect(formatMeasureValue(3, "")).toBe("3");
    expect(formatMeasureValue(null, "%")).toBe("");
  });

  it("ignores a missing or malformed measure", () => {
    expect(normalizeMeasure(null)).toBeNull();
    expect(normalizeMeasure({ direction: "up" })).toBeNull();
    expect(row("card:observability").measure).toBeNull();
  });

  it("draws a sparkline only from two or more readings", () => {
    expect(sparklinePoints([1])).toBe("");
    expect(sparklinePoints([1, 2, 3]).split(" ")).toHaveLength(3);
    expect(sparklinePoints([5, 5])).toBe("0,15 56,15");
  });

  it("demotes a late promise on a measured row", () => {
    expect(
      demotePromiseParts([
        {
          prefix: "own promise",
          text: "3h past",
          field: "initiatives.past_promise_for",
          tone: "danger",
        },
        { text: "enroll m6", field: "initiatives.promise", label: "next:" },
      ]),
    ).toEqual([
      {
        text: "reporting late 3h",
        field: "initiatives.past_promise_for",
        tone: "muted",
      },
      { text: "enroll m6", field: "initiatives.promise", label: "next:" },
    ]);
  });
});

describe("stakes, fyi and upkeep", () => {
  it("normalizes the fixture", () => {
    expect(
      brief.decisions.find((d) => d.ref === "inbox:ask-91").stakes,
    ).toEqual({ irreversible: true, cost: "$1,200 of rig time" });
    expect(brief.decisions.filter((d) => d.stakes)).toHaveLength(1);
    expect(brief.fyi).toEqual([
      {
        ref: "event:fyi-7",
        title: "Nightly backlog digest: 3 cards past promise",
        text: "3 of 41 open cards passed their promise overnight; their owners were asked to renew or close them.",
        author: "guard on host-2",
        at: "2026-10-15T03:00:00Z",
        subject: { ref: "card:observability", title: "Observability" },
        repeats: 4,
      },
    ]);
    expect(brief.upkeep).toEqual({
      open: 3,
      truncated: false,
      oldestWaiting: 777600,
      untended: true,
    });
    expect(normalizeBrief({}).fyi).toEqual([]);
    expect(normalizeBrief({}).upkeep).toBeNull();
  });

  it("names how many times a repeated fyi arrived", () => {
    expect(fyiRepeatText(0)).toBe("");
    expect(fyiRepeatText(4)).toBe("arrived 5 times");
  });

  it("warns only when core reports the workspace untended", () => {
    expect(upkeepNotice(brief.upkeep)).toEqual({ count: "3", days: 9 });
    expect(
      upkeepNotice({
        open: 50,
        truncated: true,
        oldestWaiting: 9 * 86_400,
        untended: true,
      }),
    ).toEqual({ count: "50+", days: 9 });
    expect(
      upkeepNotice({
        open: 3,
        truncated: false,
        oldestWaiting: 3 * 86_400,
        untended: false,
      }),
    ).toBeNull();
    expect(
      upkeepNotice({
        open: 0,
        truncated: false,
        oldestWaiting: 9 * 86_400,
        untended: true,
      }),
    ).toBeNull();
    expect(upkeepNotice(null)).toBeNull();
  });

  it("allows the new fields as computed sources", () => {
    for (const field of [
      "initiatives.measure",
      "decisions.stakes",
      "fyi",
      "upkeep",
    ])
      expect(BRIEF_COMPUTED_FIELDS.has(field)).toBe(true);
  });
});
