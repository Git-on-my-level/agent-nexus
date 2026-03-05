import { describe, expect, it } from "vitest";

import { toTimelineViewEvent } from "../../src/lib/timelineUtils.js";

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
        refs: ["event:evt-z", "thread:thread-1"],
        payload: {
          changed_fields: ["status", "current_summary"],
        },
      },
      { threadId: "thread-1" },
    );

    expect(view.isKnownType).toBe(true);
    expect(view.changedFields).toEqual(["status", "current_summary"]);
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
  });
});
