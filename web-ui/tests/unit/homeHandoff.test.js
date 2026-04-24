import { describe, expect, it } from "vitest";

import {
  buildHomeChangeCards,
  computeNextHomeHandoffMarker,
  filterHomeTimelineEvents,
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
          type: "decision_made",
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
          type: "decision_made",
          ts: "2026-03-12T11:30:00.000Z",
        },
      ],
    });

    expect(nextMarker).toBe("2026-03-12T12:00:00.000Z");
  });

  it("includes high-signal messages and unknown events while excluding inbox acknowledgements", () => {
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
          id: "evt-ack",
          type: "inbox_item_acknowledged",
          ts: "2026-03-12T10:00:00.000Z",
        },
        {
          id: "evt-archived",
          type: "decision_made",
          ts: "2026-03-12T11:00:00.000Z",
          archived_at: "2026-03-12T11:30:00.000Z",
        },
      ],
      { markerIso },
    );

    expect(events.map((event) => event.id)).toEqual(["evt-unknown", "evt-msg"]);
  });
});
