import { describe, expect, it } from "vitest";

import {
  buildTimelineRefLabelHints,
  normalizeDocumentRevisionsInput,
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

  it("resolves refs for message_posted events", () => {
    const view = toTimelineViewEvent(
      {
        id: "evt-y",
        type: "message_posted",
        refs: ["event:evt-z", "thread:thread-1", "document:doc-1"],
      },
      { threadId: "thread-1" },
    );

    expect(view.isKnownType).toBe(true);
    expect(view.typeLabel).toBe("Message posted");
    expect(view.resolvedRefs[0]).toMatchObject({
      kind: "event",
      href: "",
      isLink: false,
    });
    expect(view.resolvedRefs[1]).toMatchObject({
      kind: "thread",
      href: "",
      isLink: false,
    });
    expect(view.resolvedRefs[2]).toMatchObject({
      kind: "document",
      href: "",
      isLink: false,
      primaryLabel: "Document doc-1",
    });
  });

  it("routes primitive refs when timeline expansions prove a direct high-level object", () => {
    const view = toTimelineView(
      [
        {
          id: "evt-message",
          type: "message_posted",
          refs: ["artifact:rev-doc", "event:evt-parent"],
        },
        {
          id: "evt-parent",
          type: "message_posted",
          refs: [],
        },
      ],
      {
        threadId: "thread-1",
        artifacts: [
          {
            id: "rev-doc",
            kind: "doc",
            owner_ref: "document:doc-1",
          },
        ],
        documents: [{ id: "doc-1", title: "Runbook" }],
      },
    );

    const message = view.find((event) => event.id === "evt-message");
    expect(message.resolvedRefs[0]).toMatchObject({
      routed: true,
      routedKind: "document",
      primaryLabel: "Runbook",
    });
    expect(message.resolvedRefs[1]).toMatchObject({
      routed: true,
      routedKind: "message",
      primaryLabel: "Message",
    });
  });

  it("routes document head revision artifact refs from document expansion metadata", () => {
    const view = toTimelineView(
      [
        {
          id: "evt-message",
          type: "message_posted",
          refs: ["artifact:rev-doc"],
        },
      ],
      {
        threadId: "thread-1",
        documents: [
          {
            id: "doc-1",
            title: "Runbook",
            head_revision: { artifact_id: "rev-doc" },
          },
        ],
      },
    );

    expect(view[0].resolvedRefs[0]).toMatchObject({
      routed: true,
      routedKind: "document",
      primaryLabel: "Runbook",
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

  it("extracts changed_fields and preview lines for card_updated", () => {
    const view = toTimelineViewEvent(
      {
        id: "evt-c",
        type: "card_updated",
        summary: "Card updated: X",
        payload: {
          changed_fields: ["title"],
          previous_title: "A",
          title: "B",
          card_id: "c1",
        },
      },
      { threadId: "thread-1" },
    );
    expect(view.changedFields).toEqual(["title"]);
    expect(view.changePreviewLines.length).toBeGreaterThan(0);
    expect(view.changePreviewLines[0]).toMatch(/Title/i);
  });

  it("normalizes document revision list input for label hints", () => {
    const hints = buildTimelineRefLabelHints(
      {},
      { d1: { id: "d1", title: "Doc" } },
      normalizeDocumentRevisionsInput([
        { revision_id: "rv1", document_id: "d1", revision_number: 2 },
      ]),
    );
    expect(hints["document_revision:rv1"]).toBe("Doc revision 2");
  });

  it("builds label hints from timeline expansions", () => {
    const hints = buildTimelineRefLabelHints(
      {
        artifact_1: { kind: "attachment", summary: "Reproduce issue" },
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
