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

    expect(resolveRefLink("topic:topic-1")).toMatchObject({
      kind: "topic",
      href: "/topics/topic-1",
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

  it("scopes internal refs to the active workspace when provided", () => {
    expect(
      resolveRefLink("document_revision:rev-1", { workspaceSlug: "local" }),
    ).toMatchObject({
      href: "/local/docs/revisions/rev-1",
      isLink: true,
    });

    expect(
      resolveRefLink("thread:thread-1", { workspaceSlug: "local" }),
    ).toMatchObject({
      href: "/local/threads/thread-1",
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

  it("keeps event refs non-linkable when no thread context is available", () => {
    expect(resolveRefLink("event:evt-9")).toMatchObject({
      kind: "event",
      href: "",
      isExternal: false,
      isLink: false,
    });
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

    const topicRef = resolveRefLink("topic:topic-1", {
      humanize: true,
    });

    expect(topicRef).toMatchObject({
      kind: "topic",
      label: "Topic topic-1",
      primaryLabel: "Topic topic-1",
      secondaryLabel: "topic:topic-1",
      href: "/topics/topic-1",
      isLink: true,
    });

    const threadRef = resolveRefLink("thread:thread-1", {
      humanize: true,
    });

    expect(threadRef).toMatchObject({
      kind: "thread",
      label: "Thread thread-1",
      primaryLabel: "Thread thread-1",
      secondaryLabel: "thread:thread-1",
      href: "/threads/thread-1",
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

  it("truncates UUID values in humanized labels to 10 chars", () => {
    const threadRef = resolveRefLink(
      "thread:be0ef636-4ec0-4284-b65c-a868acf124be",
      { humanize: true },
    );
    expect(threadRef).toMatchObject({
      primaryLabel: "Thread be0ef636-4",
      secondaryLabel: "thread:be0ef636-4ec0-4284-b65c-a868acf124be",
    });

    const topicRef = resolveRefLink(
      "topic:a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      { humanize: true },
    );
    expect(topicRef).toMatchObject({
      primaryLabel: "Topic a1b2c3d4-e",
    });

    const nonUuidRef = resolveRefLink("thread:thread-onboarding", {
      humanize: true,
    });
    expect(nonUuidRef).toMatchObject({
      primaryLabel: "Thread thread-onboarding",
    });
  });
});
