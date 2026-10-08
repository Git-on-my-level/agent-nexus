import { describe, expect, it } from "vitest";

import {
  cardGroup,
  groupedWorkSummaryCards,
  inboxWaitingLine,
  workSummaryCard,
  workSummaryCards,
} from "../../src/lib/workSummaryCards.js";

const NOW = Date.parse("2026-10-04T12:00:00Z");

/** A card row with the computed summary core now sends. */
const row = (state, overrides = {}) => ({
  ref: "card:release-b",
  title: "Release B",
  summary: "Initiative plans, live dashboards and agent ergonomics.",
  needs: [],
  work_summary: { status: { state, label: state } },
  ...overrides,
});

describe("workSummaryCard", () => {
  const card = (overrides, options) =>
    workSummaryCard(
      { ...row("blocked"), ...overrides },
      { now: NOW, href: (ref) => `/w/${encodeURIComponent(ref)}`, ...options },
    );

  it("carries the name and the link to the task page", () => {
    expect(card()).toMatchObject({
      title: "Release B",
      href: "/w/card%3Arelease-b",
    });
  });

  it("shows the description as plain prose, never the markdown source", () => {
    expect(card({ summary: "**Goal:** ship `anx` by Friday" }).excerpt).toBe(
      "Goal: ship anx by Friday",
    );
  });

  it("reads the prose from summary_text when summary is the object", () => {
    // `summary=1` moves prose to `summary_text` and leaves the computed
    // object at `summary`; a card must not try to excerpt an object.
    expect(
      card({
        summary: { status: { state: "blocked", label: "Blocked" } },
        summary_text: "**Goal:** ship it",
      }).excerpt,
    ).toBe("Goal: ship it");
  });

  it("clips a long description rather than wrapping a card", () => {
    const long = card({ summary: "word ".repeat(80) }, { excerptLimit: 40 });
    expect(long.excerpt.length).toBeLessThanOrEqual(41);
    expect(long.excerpt.endsWith("…")).toBe(true);
  });

  it("keeps the blocked step titles as the one needs-you pill", () => {
    expect(
      card({ needs: ["Panel binding", "Adapter contract"] }).needs,
    ).toEqual(["Panel binding", "Adapter contract"]);
  });

  it("takes everything that is state from the shared summary", () => {
    expect(card().summary.status).toMatchObject({
      state: "blocked",
      tone: "danger",
    });
  });
});

describe("cardGroup", () => {
  it("sends finished and planless cards to their own blocks", () => {
    expect(cardGroup("done")).toBe("done");
    expect(cardGroup("cancelled")).toBe("done");
    expect(cardGroup("no_plan")).toBe("no_plan");
    expect(cardGroup("")).toBe("no_plan");
    for (const state of ["blocked", "at_risk", "stale", "on_track"]) {
      expect(cardGroup(state)).toBe("attention");
    }
  });
});

describe("workSummaryCards", () => {
  it("sorts by attention, worst first", () => {
    const cards = workSummaryCards(
      [
        row("on_track", { ref: "card:ok" }),
        row("stale", { ref: "card:stale" }),
        row("blocked", { ref: "card:blocked" }),
        row("at_risk", { ref: "card:risk" }),
      ],
      { now: NOW },
    );
    expect(cards.map((card) => card.ref)).toEqual([
      "card:blocked",
      "card:risk",
      "card:stale",
      "card:ok",
    ]);
  });

  it("keeps the projection's order within one state", () => {
    const cards = workSummaryCards(
      ["card:a", "card:b", "card:c"].map((ref) => row("blocked", { ref })),
      { now: NOW },
    );
    expect(cards.map((card) => card.ref)).toEqual([
      "card:a",
      "card:b",
      "card:c",
    ]);
  });

  it("sorts a state it does not know with the quiet tail", () => {
    const cards = workSummaryCards(
      [
        row("awaiting_vendor", { ref: "card:new" }),
        row("blocked", { ref: "card:blocked" }),
      ],
      { now: NOW },
    );
    expect(cards.map((card) => card.ref)).toEqual(["card:blocked", "card:new"]);
  });

  it("skips a row with no ref rather than rendering a dead card", () => {
    expect(
      workSummaryCards([row("blocked", { ref: "" }), row("blocked")], {
        now: NOW,
      }),
    ).toHaveLength(1);
  });

  it("tolerates a missing list", () => {
    expect(workSummaryCards(undefined)).toEqual([]);
    expect(workSummaryCards(null)).toEqual([]);
  });
});

describe("groupedWorkSummaryCards", () => {
  it("splits the grid from the collapsed tails", () => {
    const grouped = groupedWorkSummaryCards(
      [
        row("blocked", { ref: "card:blocked" }),
        row("on_track", { ref: "card:ok" }),
        row("no_plan", { ref: "card:none" }),
        row("done", { ref: "card:done" }),
      ],
      { now: NOW },
    );
    expect(grouped.attention.map((card) => card.ref)).toEqual([
      "card:blocked",
      "card:ok",
    ]);
    expect(grouped.done.map((card) => card.ref)).toEqual(["card:done"]);
    expect(grouped.noPlan.map((card) => card.ref)).toEqual(["card:none"]);
  });
});

describe("inboxWaitingLine", () => {
  it("summarises what is waiting as one line, without calling it all decisions", () => {
    expect(
      inboxWaitingLine({ status: "ok", count: 3, href: "/inbox" }),
    ).toEqual({
      count: 3,
      href: "/inbox",
      label: "3 items need you",
    });
  });

  it("says item in the singular", () => {
    expect(
      inboxWaitingLine({ status: "ok", count: 1, href: "/inbox" }).label,
    ).toBe("1 item needs you");
  });

  it("marks a truncated count", () => {
    expect(
      inboxWaitingLine({ status: "ok", count: 50, truncated: true, href: "/i" })
        .label,
    ).toBe("50+ items need you");
  });

  it("shows nothing when nothing waits or the read failed", () => {
    expect(inboxWaitingLine({ status: "ok", count: 0 })).toBeNull();
    expect(inboxWaitingLine({ status: "unavailable", count: 4 })).toBeNull();
    expect(inboxWaitingLine(null)).toBeNull();
  });
});
