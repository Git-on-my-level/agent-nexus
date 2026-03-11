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

    expect(resolveRefLink("document:doc-1")).toMatchObject({
      kind: "document",
      href: "/docs/doc-1",
      isLink: true,
      isExternal: false,
      primaryLabel: "Document doc-1",
    });

    expect(resolveRefLink("document_revision:rev-1")).toMatchObject({
      kind: "document_revision",
      href: "/docs/revisions/rev-1",
      isLink: true,
      isExternal: false,
      primaryLabel: "Document revision rev-1",
    });
  });

  it("preserves unknown prefixes and renders raw text without crashing", () => {
    const unknown = resolveRefLink("unknown_prefix:value-1");
    expect(unknown.kind).toBe("unknown");
    expect(unknown.label).toBe("unknown_prefix:value-1");
    expect(unknown.isLink).toBe(false);
    expect(unknown.href).toBe("");
  });

  it("can humanize labels and keep raw ids as secondary labels", () => {
    const artifactRef = resolveRefLink("artifact:artifact-1", {
      humanize: true,
      labelHints: {
        "artifact:artifact-1": "Receipt draft",
      },
    });

    expect(artifactRef).toMatchObject({
      kind: "artifact",
      label: "Receipt draft",
      primaryLabel: "Receipt draft",
      secondaryLabel: "artifact:artifact-1",
    });

    const eventRef = resolveRefLink("event:evt-9", {
      humanize: true,
      threadId: "thread-1",
    });

    expect(eventRef).toMatchObject({
      kind: "event",
      label: "Event",
      secondaryLabel: "event:evt-9",
      href: "/threads/thread-1#event-evt-9",
      isLink: true,
    });

    const documentRef = resolveRefLink("document:doc-1", {
      labelHints: {
        "document:doc-1": "Product Constitution",
      },
    });

    expect(documentRef).toMatchObject({
      kind: "document",
      label: "Product Constitution",
      primaryLabel: "Product Constitution",
      secondaryLabel: "document:doc-1",
      href: "/docs/doc-1",
      isLink: true,
    });
  });
});
