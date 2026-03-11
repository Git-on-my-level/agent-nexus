import { describe, expect, it } from "vitest";

import {
  buildTimelineRefLabelHints,
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

  it("extracts changed_fields for snapshot_updated and resolves refs", () => {
    const view = toTimelineViewEvent(
      {
        id: "evt-y",
        type: "snapshot_updated",
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

  it("builds label hints from timeline expansions", () => {
    const hints = buildTimelineRefLabelHints(
      {
        snapshot_1: { kind: "thread", title: "Incident thread" },
      },
      {
        artifact_1: { kind: "work_order", summary: "Reproduce issue" },
      },
      {
        doc_1: { title: "Product Constitution" },
      },
      {
        rev_1: { document_id: "doc_1", revision_number: 3 },
      },
    );

    expect(hints["snapshot:snapshot_1"]).toBe("Incident thread");
    expect(hints["artifact:artifact_1"]).toBe("Reproduce issue");
    expect(hints["document:doc_1"]).toBe("Product Constitution");
    expect(hints["document_revision:rev_1"]).toBe(
      "Product Constitution revision 3",
    );
  });
});
