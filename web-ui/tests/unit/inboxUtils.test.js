import { describe, expect, it } from "vitest";

import { groupInboxItems } from "../../src/lib/inboxUtils.js";
import { resolveRefLink } from "../../src/lib/refLinkModel.js";

describe("inbox grouping", () => {
  it("groups by schema category and sorts by source event time then title", () => {
    const grouped = groupInboxItems([
      {
        id: "b",
        category: "decision_needed",
        title: "Beta",
      },
      {
        id: "a",
        category: "decision_needed",
        title: "Alpha",
        source_event_time: "2026-03-03T10:00:00.000Z",
      },
      {
        id: "c",
        category: "exception",
        title: "Gamma",
      },
    ]);

    expect(grouped.map((group) => group.category)).toEqual([
      "decision_needed",
      "exception",
      "commitment_risk",
    ]);

    expect(grouped[0].items.map((item) => item.id)).toEqual(["a", "b"]);
    expect(grouped[1].items.map((item) => item.id)).toEqual(["c"]);
    expect(grouped[2].items).toHaveLength(0);
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
