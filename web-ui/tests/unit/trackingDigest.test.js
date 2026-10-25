import { describe, expect, it } from "vitest";
import { normalizeBrief, WATCH_STATES } from "../../src/lib/brief.js";
import { collectionFromDailyDigest } from "../../src/lib/collection.js";
import { planHealthModel } from "../../src/lib/planHealth.js";
import { statusRank, workSummaryModel } from "../../src/lib/workSummary.js";

describe("digest vocabulary", () => {
  it("uses ordinary collection rows for both principal-owned digest reasons", () => {
    const cards = [
      { ref: "card:lapsed", title: "Lapsed", reason: "default_promise_lapsed" },
      { ref: "card:orphan", title: "Orphan", reason: "awaiting_owner" },
    ];
    const collection = collectionFromDailyDigest(cards, {
      href: (row) => `/initiatives/${row.id}`,
    });
    expect(collection.counts.digest).toBe(2);
    expect(
      collection.groups[0].rows.map((row) => [
        row.href,
        row.subtitle.parts[0].text,
      ]),
    ).toEqual([
      ["/initiatives/lapsed", "Default promise elapsed"],
      ["/initiatives/orphan", "Awaiting owner"],
    ]);
    for (const row of collection.groups[0].rows) {
      expect(row.state).toBe("digest");
      expect(row.stateField).toBe("daily_digest.reason");
    }
  });
  it("preserves digest instead of inventing a missing promise", () => {
    const row = normalizeBrief({
      initiatives: [{ ref: "card:quiet", title: "Quiet", state: "digest" }],
    }).initiatives[0];
    expect(row.state).toBe("digest");
    expect(WATCH_STATES.indexOf("digest")).toBeLessThan(
      WATCH_STATES.indexOf("done"),
    );
    expect(
      collectionFromDailyDigest([
        { ref: row.ref, title: row.title, reason: "awaiting_owner" },
      ]).counts.digest,
    ).toBe(1);
    const health = planHealthModel({
      plan_health: { state: "digest", reason: "Daily owner digest" },
    });
    expect(health).toMatchObject({
      state: "digest",
      known: true,
      tone: "neutral",
    });
    const summary = workSummaryModel({
      work_summary: { status: { state: "digest", label: "Daily digest" } },
    });
    expect(summary.status).toMatchObject({
      state: "digest",
      tone: "neutral",
      glyph: "○",
    });
    expect(statusRank("digest")).toBeGreaterThan(statusRank("moving"));
    expect(statusRank("digest")).toBeLessThan(statusRank("done"));
  });
});
