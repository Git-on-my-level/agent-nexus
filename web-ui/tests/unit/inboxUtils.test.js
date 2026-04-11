import { describe, expect, it } from "vitest";

import {
  decisionGroundingRefForInboxItem,
  deriveInboxUrgency,
  enrichInboxItem,
  formatInboxItemBoardPanelResourceLine,
  getInboxSubjectRef,
  getInboxUrgencyLabel,
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
          id: "new-action",
          category: "action_needed",
          title: "Action just raised",
          source_event_time: "2026-03-07T11:00:00.000Z",
        },
        {
          id: "old-risk",
          category: "risk_exception",
          title: "Aging risk",
          source_event_time: "2026-03-03T10:00:00.000Z",
        },
        {
          id: "old-action",
          category: "action_needed",
          source_event_time: "2026-03-03T10:00:00.000Z",
          title: "Action waiting for days",
        },
        {
          id: "fresh-risk",
          category: "risk_exception",
          source_event_time: "2026-03-07T10:00:00.000Z",
          title: "Fresh exception",
        },
      ],
      { now },
    );

    expect(grouped.map((group) => group.category)).toEqual([
      "action_needed",
      "risk_exception",
      "attention",
    ]);

    expect(grouped[0].items.map((item) => item.id)).toEqual([
      "old-action",
      "new-action",
    ]);
    expect(grouped[1].items.map((item) => item.id)).toEqual([
      "old-risk",
      "fresh-risk",
    ]);
    expect(grouped[2].items).toEqual([]);
  });
});

describe("inbox urgency derivation", () => {
  it("derives urgency level from category + source event age", () => {
    const now = "2026-03-07T12:00:00.000Z";
    // risk_exception base=84, aged 8h → 84+6=90 → immediate
    const immediate = deriveInboxUrgency(
      {
        category: "risk_exception",
        source_event_time: "2026-03-07T04:00:00.000Z",
      },
      { now },
    );
    // action_needed base=76, fresh → 76 → high (>=74)
    const high = deriveInboxUrgency(
      {
        category: "action_needed",
        source_event_time: "2026-03-07T11:30:00.000Z",
      },
      { now },
    );
    // attention base=58, fresh → 58 → normal
    const normal = deriveInboxUrgency(
      {
        category: "attention",
        source_event_time: "2026-03-07T11:30:00.000Z",
      },
      { now },
    );

    expect(immediate.level).toBe("immediate");
    expect(high.level).toBe("high");
    expect(normal.level).toBe("normal");
  });

  it("parses ISO now values when computing age-based urgency boosts", () => {
    // action_needed base=76, aged 26h → 76+10=86 → high
    const urgency = deriveInboxUrgency(
      {
        category: "action_needed",
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
        category: "risk_exception",
        source_event_time: "2026-03-07T04:00:00.000Z", // 8h old → 84+6=90 → immediate
      },
      {
        id: "2",
        category: "action_needed",
        source_event_time: "2026-03-07T11:00:00.000Z", // fresh → 76 → high
      },
      {
        id: "3",
        category: "attention", // no timestamp → base 58 → normal
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

  it("keeps unknown urgency labels inspectable", () => {
    expect(getInboxUrgencyLabel("needs_triage")).toBe("needs_triage");
    expect(getInboxUrgencyLabel("")).toBe("Unknown");
  });
});

describe("inbox typed-ref rendering targets", () => {
  it("derives thread grounding ref for decision flows from inbox data", () => {
    expect(
      decisionGroundingRefForInboxItem({
        id: "in-1",
        thread_id: "thread-pricing-glitch",
        related_refs: [
          "thread:thread-pricing-glitch",
          "artifact:artifact-pricing-evidence",
        ],
      }),
    ).toBe("thread:thread-pricing-glitch");

    expect(
      decisionGroundingRefForInboxItem({
        id: "in-1b",
        subject_ref: "artifact:artifact-pricing-evidence",
        thread_id: "thread-pricing-glitch",
        related_refs: ["artifact:artifact-pricing-evidence"],
      }),
    ).toBe("thread:thread-pricing-glitch");

    expect(
      decisionGroundingRefForInboxItem({
        id: "in-1c",
        subject_ref: "artifact:artifact-pricing-evidence",
        related_refs: [
          "thread:thread-pricing-glitch",
          "artifact:artifact-pricing-evidence",
        ],
      }),
    ).toBe("thread:thread-pricing-glitch");

    expect(
      decisionGroundingRefForInboxItem({
        id: "in-2",
        related_refs: ["topic:real-topic-id", "thread:thr-1"],
      }),
    ).toBe("thread:thr-1");

    expect(
      decisionGroundingRefForInboxItem({
        id: "in-3",
        subject_ref: "thread:thr-77",
        related_refs: [],
      }),
    ).toBe("thread:thr-77");

    expect(
      decisionGroundingRefForInboxItem({
        id: "in-4",
        subject_ref: "artifact:artifact-1",
        thread_id: "thr-x",
        related_refs: [],
      }),
    ).toBe("thread:thr-x");

    expect(
      decisionGroundingRefForInboxItem({ id: "in-5", related_refs: [] }),
    ).toBe("");

    expect(
      decisionGroundingRefForInboxItem({
        id: "in-legacy-shape",
        refs: ["thread:thread-only-in-refs"],
        related_refs: [],
      }),
    ).toBe("");
  });

  it("preserves explicit subject refs and prefers specific ids before thread fallback", () => {
    expect(
      getInboxSubjectRef({
        subject_ref: "topic:topic-123",
        topic_id: "topic-999",
        thread_id: "thread-999",
      }),
    ).toBe("topic:topic-123");

    expect(
      getInboxSubjectRef({
        topic_id: "topic-123",
        thread_id: "thread-123",
      }),
    ).toBe("topic:topic-123");

    expect(
      getInboxSubjectRef({
        card_id: "card-123",
        thread_id: "thread-123",
      }),
    ).toBe("card:card-123");

    expect(
      getInboxSubjectRef({
        thread_id: "thread-123",
      }),
    ).toBe("thread:thread-123");
  });

  it("formats board panel resource line from subject_ref, not misleading topic_id", () => {
    const cardAnchored = enrichInboxItem({
      id: "1",
      category: "risk_exception",
      subject_ref: "card:card-1",
      title: "Risk",
      topic_id: "topic-extra",
      thread_id: "thread-extra",
    });
    expect(formatInboxItemBoardPanelResourceLine(cardAnchored)).toBe(
      "Card card-1",
    );

    const topicAnchored = enrichInboxItem({
      id: "2",
      category: "risk_exception",
      subject_ref: "topic:topic-1",
      title: "Stale",
      thread_id: "thread-1",
    });
    expect(formatInboxItemBoardPanelResourceLine(topicAnchored)).toBe(
      "Topic topic-1",
    );

    const threadAnchored = enrichInboxItem({
      id: "3",
      category: "action_needed",
      subject_ref: "thread:thread-1",
      title: "Decide",
      topic_id: "topic-1",
    });
    expect(formatInboxItemBoardPanelResourceLine(threadAnchored)).toBe(
      "Thread thread-1",
    );

    expect(
      formatInboxItemBoardPanelResourceLine(
        enrichInboxItem({
          id: "4",
          category: "attention",
          subject_ref: "document:doc-1",
          title: "Doc",
        }),
      ),
    ).toBe("Document doc-1");

    expect(
      formatInboxItemBoardPanelResourceLine(
        enrichInboxItem({
          id: "5",
          thread_id: "thread-only",
          category: "risk_exception",
          title: "Legacy",
        }),
      ),
    ).toBe("Thread thread-only");
  });

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
