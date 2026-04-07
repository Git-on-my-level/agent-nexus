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
          id: "new-decision",
          category: "decision_needed",
          title: "Decision just raised",
          source_event_time: "2026-03-07T11:00:00.000Z",
        },
        {
          id: "old-risk",
          category: "work_item_risk",
          title: "Aging risk",
          source_event_time: "2026-03-03T10:00:00.000Z",
        },
        {
          id: "new-intervention",
          category: "intervention_needed",
          title: "Human needs to publish the approved post",
          source_event_time: "2026-03-07T11:30:00.000Z",
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
      "intervention_needed",
      "work_item_risk",
      "stale_topic",
      "document_attention",
    ]);

    expect(grouped[0].items.map((item) => item.id)).toEqual([
      "old-decision",
      "new-decision",
    ]);
    expect(grouped[1].items.map((item) => item.id)).toEqual([
      "new-intervention",
    ]);
    expect(grouped[2].items.map((item) => item.id)).toEqual(["old-risk"]);
    expect(grouped[3].items.map((item) => item.id)).toEqual([
      "fresh-exception",
    ]);
    expect(grouped[4].items).toEqual([]);
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
        category: "work_item_risk",
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
        category: "work_item_risk",
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
      category: "work_item_risk",
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
      category: "stale_topic",
      subject_ref: "topic:topic-1",
      title: "Stale",
      thread_id: "thread-1",
    });
    expect(formatInboxItemBoardPanelResourceLine(topicAnchored)).toBe(
      "Topic topic-1",
    );

    const threadAnchored = enrichInboxItem({
      id: "3",
      category: "decision_needed",
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
          category: "document_attention",
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
          category: "stale_topic",
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
