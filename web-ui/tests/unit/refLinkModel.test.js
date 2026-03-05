import { describe, expect, it } from "vitest";

import { resolveRefLink } from "../../src/lib/refLinkModel.js";

describe("RefLink model", () => {
  it("resolves known typed refs into deterministic targets", () => {
    expect(resolveRefLink("artifact:artifact-1")).toMatchObject({
      kind: "artifact",
      href: "/artifacts/artifact-1",
      isLink: true,
      isExternal: false,
    });

    expect(resolveRefLink("thread:thread-1")).toMatchObject({
      kind: "thread",
      href: "/threads/thread-1",
      isLink: true,
    });

    expect(resolveRefLink("snapshot:snap-1")).toMatchObject({
      kind: "snapshot",
      href: "/snapshots/snap-1",
      isLink: true,
    });

    expect(
      resolveRefLink("event:evt-9", { threadId: "thread-1" }),
    ).toMatchObject({
      kind: "event",
      href: "/threads/thread-1#event-evt-9",
      isLink: true,
    });

    expect(resolveRefLink("url:https://example.com/a")).toMatchObject({
      kind: "url",
      href: "https://example.com/a",
      isExternal: true,
      isLink: true,
    });

    expect(resolveRefLink("inbox:item-2")).toMatchObject({
      kind: "inbox",
      href: "/inbox#inbox-item-2",
      isLink: true,
    });
  });

  it("preserves unknown prefixes and renders raw text without crashing", () => {
    const unknown = resolveRefLink("unknown_prefix:value-1");
    expect(unknown.kind).toBe("unknown");
    expect(unknown.label).toBe("unknown_prefix:value-1");
    expect(unknown.isLink).toBe(false);
    expect(unknown.href).toBe("");
  });
});
