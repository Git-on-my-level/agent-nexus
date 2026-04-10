import { describe, expect, it } from "vitest";

import {
  buildTimelineRefLabelHints,
  toTimelineView,
  toTimelineViewEvent,
} from "../../src/lib/timelineUtils.js";

describe("timeline utils", () => {
  it("marks unknown event types and preserves raw payload/refs", () => {
    const view = toTimelineViewEvent(
      {
        id: "evt-x",
        type: "future_custom_type",
        refs: ["mystery:opaque"],
        payload: { score: 7 },
      },
      { threadId: "thread-1" },
    );

    expect(view.isKnownType).toBe(false);
    expect(view.typeLabel).toBe("Unknown event type");
    expect(view.rawType).toBe("future_custom_type");
    expect(view.resolvedRefs[0]).toMatchObject({
      kind: "unknown",
      label: "mystery:opaque",
      isLink: false,
    });
  });

  it("extracts changed_fields for thread_updated and resolves refs", () => {
    const view = toTimelineViewEvent(
      {
        id: "evt-y",
        type: "thread_updated",
        refs: ["event:evt-z", "thread:thread-1", "document:doc-1"],
        payload: {
          changed_fields: ["status", "current_summary"],
        },
      },
      { threadId: "thread-1" },
    );

    expect(view.isKnownType).toBe(true);
    expect(view.changedFields).toEqual(["Status", "Summary"]);
    expect(view.resolvedRefs[0]).toMatchObject({
      kind: "event",
      href: "/threads/thread-1#event-evt-z",
      isLink: true,
    });
    expect(view.resolvedRefs[1]).toMatchObject({
      kind: "thread",
      href: "/threads/thread-1",
      isLink: true,
    });
    expect(view.resolvedRefs[2]).toMatchObject({
      kind: "document",
      href: "/docs/doc-1",
      isLink: true,
      primaryLabel: "Document doc-1",
    });
  });

  it("orders timeline view newest first with stable id tie-break", () => {
    const view = toTimelineView(
      [
        {
          id: "evt-old",
          ts: "2026-03-01T00:00:00.000Z",
          type: "message_posted",
          summary: "older",
        },
        {
          id: "evt-new",
          ts: "2026-03-03T00:00:00.000Z",
          type: "message_posted",
          summary: "newer",
        },
      ],
      { threadId: "thread-1" },
    );
    expect(view.map((e) => e.id)).toEqual(["evt-new", "evt-old"]);

    const sameTs = toTimelineView(
      [
        { id: "evt-b", ts: "2026-03-03T12:00:00.000Z", type: "message_posted" },
        { id: "evt-a", ts: "2026-03-03T12:00:00.000Z", type: "message_posted" },
      ],
      { threadId: "thread-1" },
    );
    expect(sameTs.map((e) => e.id)).toEqual(["evt-b", "evt-a"]);
  });

  it("builds label hints from timeline expansions", () => {
    const hints = buildTimelineRefLabelHints(
      {
        artifact_1: { kind: "receipt", summary: "Reproduce issue" },
      },
      {
        doc_1: { title: "Product Constitution" },
      },
      {
        rev_1: { document_id: "doc_1", revision_number: 3 },
      },
    );

    expect(hints["artifact:artifact_1"]).toBe("Reproduce issue");
    expect(hints["document:doc_1"]).toBe("Product Constitution");
    expect(hints["document_revision:rev_1"]).toBe(
      "Product Constitution revision 3",
    );
  });
});
