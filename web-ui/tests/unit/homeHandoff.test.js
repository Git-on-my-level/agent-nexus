import { describe, expect, it } from "vitest";

import {
  buildHomeChangeCards,
  buildHomeRefLabelHints,
  computeNextHomeHandoffMarker,
  filterHomeTimelineEvents,
  homeHandoffEventPillId,
  homeTimelinePrimaryRefFromEvent,
  homeTimelinePrimaryRefFromRefs,
} from "../../src/lib/homeHandoff.js";

describe("homeHandoff helpers", () => {
  const markerIso = "2026-03-12T02:00:00.000Z";

  it("counts everything available when no marker exists", () => {
    const cards = buildHomeChangeCards({
      inboxItems: [
        { id: "inbox-a", source_event_time: "2026-03-11T23:00:00.000Z" },
        { id: "inbox-b", source_event_time: "2026-03-12T03:00:00.000Z" },
      ],
      topics: [
        { id: "topic-a", updated_at: "2026-03-10T03:00:00.000Z" },
        { id: "topic-b", updated_at: "2026-03-12T05:00:00.000Z" },
      ],
      boards: [{ id: "board-a", updated_at: "2026-03-11T12:00:00.000Z" }],
      documents: [{ id: "doc-a", updated_at: "2026-03-09T12:00:00.000Z" }],
      artifacts: [{ id: "artifact-a", created_at: "2026-03-08T12:00:00.000Z" }],
    });

    expect(cards).toEqual([
      { id: "inbox", label: "Inbox changes", count: 2 },
      { id: "topics", label: "Topic changes", count: 2 },
      { id: "boards", label: "Board changes", count: 1 },
      { id: "docs-proof", label: "Docs / Proof changes", count: 2 },
    ]);
  });

  it("counts only entities newer than the handoff marker", () => {
    const cards = buildHomeChangeCards({
      markerIso,
      inboxItems: [
        { id: "inbox-old", source_event_time: "2026-03-12T01:00:00.000Z" },
        { id: "inbox-new", source_event_time: "2026-03-12T03:00:00.000Z" },
      ],
      topics: [
        { id: "topic-old", updated_at: "2026-03-11T20:00:00.000Z" },
        { id: "topic-new", updated_at: "2026-03-12T04:00:00.000Z" },
      ],
      boards: [
        { id: "board-old", updated_at: "2026-03-11T20:00:00.000Z" },
        { id: "board-new", updated_at: "2026-03-12T06:00:00.000Z" },
      ],
      documents: [
        { id: "doc-old", updated_at: "2026-03-11T22:00:00.000Z" },
        { id: "doc-new", updated_at: "2026-03-12T07:00:00.000Z" },
      ],
      artifacts: [
        { id: "artifact-old", created_at: "2026-03-12T01:30:00.000Z" },
        { id: "artifact-new", created_at: "2026-03-12T08:00:00.000Z" },
      ],
    });

    expect(cards).toEqual([
      { id: "inbox", label: "Inbox changes", count: 1 },
      { id: "topics", label: "Topic changes", count: 1 },
      { id: "boards", label: "Board changes", count: 1 },
      { id: "docs-proof", label: "Docs / Proof changes", count: 2 },
    ]);
  });

  it("counts topic surfaces from message_posted when topic updated_at lags the chat", () => {
    const cards = buildHomeChangeCards({
      markerIso,
      events: [
        {
          id: "msg-1",
          type: "message_posted",
          ts: "2026-03-12T14:00:00.000Z",
          thread_id: "thread-a",
        },
        {
          id: "msg-2",
          type: "message_posted",
          ts: "2026-03-12T15:00:00.000Z",
          thread_id: "thread-a",
        },
      ],
      topics: [
        {
          id: "topic-a",
          thread_id: "thread-a",
          updated_at: "2026-03-11T20:00:00.000Z",
        },
      ],
      inboxItems: [],
      boards: [],
      documents: [],
      artifacts: [],
    });

    expect(cards.find((c) => c.id === "topics")?.count).toBe(1);
  });

  it("uses the latest visible timestamp for mark-as-read instead of unconditional now", () => {
    const nextMarker = computeNextHomeHandoffMarker({
      markerIso: "",
      now: Date.parse("2026-03-12T14:00:00.000Z"),
      inboxItems: [
        { id: "inbox-a", source_event_time: "2026-03-12T09:00:00.000Z" },
      ],
      topics: [{ id: "topic-a", updated_at: "2026-03-12T10:00:00.000Z" }],
      boards: [],
      documents: [],
      artifacts: [],
      events: [
        {
          id: "evt-newest",
          type: "message_posted",
          ts: "2026-03-12T11:30:00.000Z",
        },
      ],
    });

    expect(nextMarker).toBe("2026-03-12T11:30:00.000Z");
  });

  it("never moves the handoff marker backward on a zero-change page", () => {
    const nextMarker = computeNextHomeHandoffMarker({
      markerIso: "2026-03-12T12:00:00.000Z",
      now: Date.parse("2026-03-12T14:00:00.000Z"),
      inboxItems: [
        { id: "inbox-a", source_event_time: "2026-03-12T09:00:00.000Z" },
      ],
      topics: [{ id: "topic-a", updated_at: "2026-03-12T10:00:00.000Z" }],
      boards: [],
      documents: [],
      artifacts: [],
      events: [
        {
          id: "evt-old",
          type: "message_posted",
          ts: "2026-03-12T11:30:00.000Z",
        },
      ],
    });

    expect(nextMarker).toBe("2026-03-12T12:00:00.000Z");
  });

  it("includes high-signal messages and unknown events while excluding system and human attention events", () => {
    const events = filterHomeTimelineEvents(
      [
        {
          id: "evt-msg",
          type: "message_posted",
          ts: "2026-03-12T08:00:00.000Z",
        },
        {
          id: "evt-unknown",
          type: "future_signal_emitted",
          ts: "2026-03-12T09:00:00.000Z",
        },
        {
          id: "evt-human-response",
          type: "human_attention_responded",
          ts: "2026-03-12T10:00:00.000Z",
        },
        {
          id: "evt-human-request",
          type: "human_attention_requested",
          ts: "2026-03-12T10:30:00.000Z",
        },
        {
          id: "evt-bridge-checkin",
          type: "agent_bridge_checked_in",
          ts: "2026-03-12T10:45:00.000Z",
        },
        {
          id: "evt-archived",
          type: "message_posted",
          ts: "2026-03-12T11:00:00.000Z",
          archived_at: "2026-03-12T11:30:00.000Z",
        },
      ],
      { markerIso },
    );

    expect(events.map((event) => event.id)).toEqual(["evt-unknown", "evt-msg"]);
  });

  it("classifies events into Home pill filters from refs in priority order", () => {
    expect(
      homeHandoffEventPillId({
        refs: ["board:b1", "thread:t1"],
        type: "card_updated",
      }),
    ).toBe("boards");
    expect(
      homeHandoffEventPillId({ refs: ["board:b1"], type: "card_updated" }),
    ).toBe("boards");
    expect(
      homeHandoffEventPillId({
        refs: ["inbox:n1"],
        type: "human_attention_requested",
      }),
    ).toBe("inbox");
    expect(
      homeHandoffEventPillId({ refs: ["document:d1"], type: "message_posted" }),
    ).toBe("docs-proof");
    expect(
      homeHandoffEventPillId({
        refs: ["thread:t1", "document:d1"],
        type: "document_updated",
      }),
    ).toBe("docs-proof");
  });

  it("picks document over thread for primary ref when both are present", () => {
    expect(homeTimelinePrimaryRefFromRefs(["thread:t1", "document:d1"])).toBe(
      "document:d1",
    );
    expect(
      homeTimelinePrimaryRefFromEvent({
        refs: ["thread:t1", "document:d1"],
        type: "document_updated",
      }),
    ).toBe("document:d1");
  });

  it("picks card over board and thread for card lifecycle refs", () => {
    expect(
      homeTimelinePrimaryRefFromRefs(["board:b1", "card:c1", "thread:t1"]),
    ).toBe("card:c1");
  });

  it("lets topic thread hints win over document-backed thread hints", () => {
    const hints = buildHomeRefLabelHints({
      topics: [{ id: "top-1", thread_id: "thr-shared", title: "Topic title" }],
      documents: [{ id: "doc-1", thread_id: "thr-shared", title: "Doc title" }],
      boards: [],
      artifacts: [],
      cards: [],
    });
    expect(hints["thread:thr-shared"]).toBe("Topic title");
  });

  it("fills thread hint from document backing id when no topic owns that thread", () => {
    const hints = buildHomeRefLabelHints({
      topics: [],
      documents: [{ id: "doc-1", thread_id: "thr-doc", title: "Hello World" }],
      boards: [],
      artifacts: [],
      cards: [],
    });
    expect(hints["thread:thr-doc"]).toBe("Hello World");
    expect(hints["document:doc-1"]).toBe("Hello World");
  });

  it("adds card title hints", () => {
    const hints = buildHomeRefLabelHints({
      topics: [],
      boards: [],
      documents: [],
      artifacts: [],
      cards: [{ id: "card-1", title: "Fix login" }],
    });
    expect(hints["card:card-1"]).toBe("Fix login");
  });

  it("uses card id as hint when title is empty", () => {
    const hints = buildHomeRefLabelHints({
      topics: [],
      boards: [],
      documents: [],
      artifacts: [],
      cards: [{ id: "card-empty", title: "" }],
    });
    expect(hints["card:card-empty"]).toBe("card-empty");
  });

  it("prefers document over document_revision for primary ref when both exist", () => {
    expect(
      homeTimelinePrimaryRefFromRefs([
        "document_revision:rev-1",
        "document:doc-1",
      ]),
    ).toBe("document:doc-1");
    expect(
      homeHandoffEventPillId({
        refs: ["document_revision:rev-1", "document:doc-1"],
        type: "document_updated",
      }),
    ).toBe("docs-proof");
  });

  it("classifies message_posted with thread as topics when refs are empty", () => {
    expect(
      homeHandoffEventPillId({
        type: "message_posted",
        thread_id: "thread-1",
        ts: "2026-03-12T10:00:00.000Z",
      }),
    ).toBe("topics");
  });
});
