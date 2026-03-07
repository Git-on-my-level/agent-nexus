import { describe, expect, it } from "vitest";

import {
  deriveInboxUrgency,
  enrichInboxItem,
  groupInboxItems,
  summarizeInboxUrgency,
} from "../../src/lib/inboxUtils.js";
import { resolveRefLink } from "../../src/lib/refLinkModel.js";

describe("inbox grouping", () => {
  it("groups by schema category and sorts by inferred urgency then age", () => {
    const now = "2026-03-07T12:00:00.000Z";
    const grouped = groupInboxItems(
      [
        {
          id: "new-decision",
          category: "decision_needed",
          title: "Decision just raised",
          source_event_time: "2026-03-07T11:00:00.000Z",
        },
        {
          id: "old-risk",
          category: "commitment_risk",
          title: "Aging risk",
          source_event_time: "2026-03-03T10:00:00.000Z",
        },
        {
          id: "old-decision",
          category: "decision_needed",
          source_event_time: "2026-03-03T10:00:00.000Z",
          title: "Decision waiting for days",
        },
        {
          id: "fresh-exception",
          category: "exception",
          source_event_time: "2026-03-07T10:00:00.000Z",
          title: "Fresh exception",
        },
      ],
      { now },
    );

    expect(grouped.map((group) => group.category)).toEqual([
      "decision_needed",
      "exception",
      "commitment_risk",
    ]);

    expect(grouped[0].items.map((item) => item.id)).toEqual([
      "old-decision",
      "new-decision",
    ]);
    expect(grouped[1].items.map((item) => item.id)).toEqual([
      "fresh-exception",
    ]);
    expect(grouped[2].items.map((item) => item.id)).toEqual(["old-risk"]);
  });
});

describe("inbox urgency derivation", () => {
  it("derives urgency level from category + source event age", () => {
    const now = "2026-03-07T12:00:00.000Z";
    const immediate = deriveInboxUrgency(
      {
        category: "exception",
        source_event_time: "2026-03-07T10:00:00.000Z",
      },
      { now },
    );
    const high = deriveInboxUrgency(
      {
        category: "decision_needed",
        source_event_time: "2026-03-07T11:30:00.000Z",
      },
      { now },
    );
    const normal = deriveInboxUrgency(
      {
        category: "commitment_risk",
        source_event_time: "2026-03-07T11:30:00.000Z",
      },
      { now },
    );

    expect(immediate.level).toBe("immediate");
    expect(high.level).toBe("high");
    expect(normal.level).toBe("normal");
  });

  it("parses ISO now values when computing age-based urgency boosts", () => {
    const urgency = deriveInboxUrgency(
      {
        category: "decision_needed",
        source_event_time: "2026-03-06T10:00:00.000Z",
      },
      { now: "2026-03-07T12:00:00.000Z" },
    );

    expect(urgency.ageHours).toBe(26);
    expect(urgency.score).toBe(86);
    expect(urgency.level).toBe("high");
  });

  it("enriches items and summarizes urgency counts", () => {
    const now = "2026-03-07T12:00:00.000Z";
    const items = [
      {
        id: "1",
        category: "exception",
        source_event_time: "2026-03-07T10:00:00.000Z",
      },
      {
        id: "2",
        category: "decision_needed",
        source_event_time: "2026-03-07T11:00:00.000Z",
      },
      {
        id: "3",
        category: "commitment_risk",
      },
    ];

    expect(enrichInboxItem(items[0], { now })).toMatchObject({
      id: "1",
      urgency_level: "immediate",
      urgency_inferred_from: "category + source event age",
    });

    expect(summarizeInboxUrgency(items, { now })).toEqual({
      immediate: 1,
      high: 1,
      normal: 1,
    });
  });
});

describe("inbox typed-ref rendering targets", () => {
  it("resolves thread/event/url refs used by inbox cards", () => {
    expect(resolveRefLink("thread:thread-onboarding")).toMatchObject({
      href: "/threads/thread-onboarding",
      isLink: true,
    });

    expect(
      resolveRefLink("event:evt-1001", { threadId: "thread-onboarding" }),
    ).toMatchObject({
      href: "/threads/thread-onboarding#event-evt-1001",
      isLink: true,
    });

    expect(resolveRefLink("url:https://example.com/reference")).toMatchObject({
      href: "https://example.com/reference",
      isExternal: true,
      isLink: true,
    });

    expect(resolveRefLink("mystery:opaque")).toMatchObject({
      isLink: false,
      label: "mystery:opaque",
    });
  });
});
