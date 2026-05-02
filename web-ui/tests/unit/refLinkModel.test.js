import { describe, expect, it } from "vitest";

import {
  buildPrimitiveRefRoutes,
  resolveRefLink,
} from "../../src/lib/refLinkModel.js";

describe("RefLink model", () => {
  it("resolves known typed refs into deterministic targets", () => {
    expect(resolveRefLink("artifact:artifact-1")).toMatchObject({
      kind: "artifact",
      href: "",
      isLink: false,
      isExternal: false,
    });

    expect(resolveRefLink("thread:thread-1")).toMatchObject({
      kind: "thread",
      href: "",
      isLink: false,
    });

    expect(resolveRefLink("topic:topic-1")).toMatchObject({
      kind: "topic",
      href: "",
      isLink: false,
    });

    expect(
      resolveRefLink("event:evt-9", { threadId: "thread-1" }),
    ).toMatchObject({
      kind: "event",
      href: "",
      isLink: false,
    });

    expect(resolveRefLink("url:https://example.com/a")).toMatchObject({
      kind: "url",
      href: "https://example.com/a",
      isExternal: true,
      isLink: true,
    });

    expect(resolveRefLink("inbox:item-2")).toMatchObject({
      kind: "inbox",
      href: "",
      isLink: false,
    });

    expect(resolveRefLink("document:doc-1")).toMatchObject({
      kind: "document",
      href: "",
      isLink: false,
      isExternal: false,
      primaryLabel: "Document doc-1",
    });

    expect(resolveRefLink("document_revision:rev-1")).toMatchObject({
      kind: "document_revision",
      href: "",
      isLink: false,
      isExternal: false,
      primaryLabel: "Document revision rev-1",
    });
  });

  it("scopes internal refs to the active workspace when provided", () => {
    expect(
      resolveRefLink("artifact:artifact-1", {
        organizationSlug: "acme",
        workspaceSlug: "proj",
      }),
    ).toMatchObject({
      href: "/o/acme/w/proj/artifacts/artifact-1",
      isLink: true,
    });

    expect(
      resolveRefLink("event:evt-9", {
        organizationSlug: "acme",
        workspaceSlug: "proj",
      }),
    ).toMatchObject({
      href: "/o/acme/w/proj/events#evt-9",
      isLink: true,
    });

    expect(
      resolveRefLink("document_revision:rev-1", {
        organizationSlug: "acme",
        workspaceSlug: "proj",
      }),
    ).toMatchObject({
      href: "/o/acme/w/proj/docs/revisions/rev-1",
      isLink: true,
    });

    expect(
      resolveRefLink("thread:thread-1", {
        organizationSlug: "acme",
        workspaceSlug: "proj",
      }),
    ).toMatchObject({
      href: "/o/acme/w/proj/threads/thread-1",
      isLink: true,
    });
  });

  it("returns isLink:false when organizationSlug is provided but workspaceSlug is missing", () => {
    expect(
      resolveRefLink("thread:thread-1", { organizationSlug: "acme" }),
    ).toMatchObject({
      kind: "thread",
      href: "",
      isLink: false,
    });
  });

  it("returns isLink:false when workspaceSlug is provided but organizationSlug is missing", () => {
    expect(
      resolveRefLink("thread:thread-1", { workspaceSlug: "proj" }),
    ).toMatchObject({
      kind: "thread",
      href: "",
      isLink: false,
    });
  });

  it("preserves unknown prefixes and renders raw text without crashing", () => {
    const unknown = resolveRefLink("unknown_prefix:value-1");
    expect(unknown.kind).toBe("unknown");
    expect(unknown.label).toBe("unknown_prefix:value-1");
    expect(unknown.isLink).toBe(false);
    expect(unknown.href).toBe("");
  });

  it("keeps event refs non-linkable when no workspace context is available", () => {
    expect(resolveRefLink("event:evt-9")).toMatchObject({
      kind: "event",
      href: "",
      isExternal: false,
      isLink: false,
    });
  });

  it("routes doc artifacts to their document target when direct ownership is known", () => {
    const ref = resolveRefLink("artifact:rev-1", {
      organizationSlug: "acme",
      workspaceSlug: "proj",
      humanize: true,
      artifactRoutesById: {
        "rev-1": {
          kind: "document",
          targetPrefix: "document",
          targetValue: "doc-1",
          label: "Launch Brief",
        },
      },
    });

    expect(ref).toMatchObject({
      raw: "artifact:rev-1",
      kind: "artifact",
      routed: true,
      routedKind: "document",
      routedPrefix: "document",
      routedValue: "doc-1",
      primaryLabel: "Launch Brief",
      secondaryLabel: "",
      href: "/o/acme/w/proj/docs/doc-1",
      isLink: true,
    });
  });

  it("routes card artifacts to their board card target when direct ownership is known", () => {
    const ref = resolveRefLink("artifact:card-rev-1", {
      organizationSlug: "acme",
      workspaceSlug: "proj",
      humanize: true,
      artifactRoutesById: {
        "artifact:card-rev-1": {
          kind: "card",
          targetPrefix: "card",
          targetValue: "card-1",
          boardId: "board-1",
          label: "Restock lemons",
        },
      },
    });

    expect(ref).toMatchObject({
      routed: true,
      routedKind: "card",
      routedPrefix: "card",
      routedValue: "card-1",
      primaryLabel: "Restock lemons",
      secondaryLabel: "",
      href: "/o/acme/w/proj/boards/board-1?card=card-1",
      isLink: true,
    });
  });

  it("routes message events to message anchors and leaves other events on Events", () => {
    const message = resolveRefLink("event:evt-message", {
      organizationSlug: "acme",
      workspaceSlug: "proj",
      humanize: true,
      eventRoutesById: {
        "evt-message": {
          kind: "message",
          type: "message_posted",
          threadId: "thread-1",
        },
      },
    });

    expect(message).toMatchObject({
      routed: true,
      routedKind: "message",
      routedPrefix: "message",
      routedValue: "evt-message",
      primaryLabel: "Message",
      secondaryLabel: "",
      href: "/o/acme/w/proj/threads/thread-1?tab=messages#message-evt-message",
      isLink: true,
    });

    const lifecycle = resolveRefLink("event:evt-life", {
      organizationSlug: "acme",
      workspaceSlug: "proj",
      humanize: true,
      eventRoutesById: {
        "evt-life": {
          type: "topic_lifecycle_changed",
        },
      },
    });

    expect(lifecycle).toMatchObject({
      kind: "event",
      primaryLabel: "Event",
      secondaryLabel: "event:evt-life",
      href: "/o/acme/w/proj/events#evt-life",
      isLink: true,
    });
  });

  it("prefers topic message targets when event route metadata includes a topic", () => {
    const message = resolveRefLink("event:evt-message", {
      organizationSlug: "acme",
      workspaceSlug: "proj",
      humanize: true,
      eventRoutesById: {
        "evt-message": {
          kind: "message",
          type: "message_posted",
          topicId: "topic-1",
          threadId: "thread-1",
        },
      },
    });

    expect(message).toMatchObject({
      routed: true,
      routedKind: "message",
      href: "/o/acme/w/proj/topics/topic-1?tab=messages#message-evt-message",
      isLink: true,
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
      href: "",
      isLink: false,
    });

    const eventRef = resolveRefLink("event:evt-9", {
      humanize: true,
      threadId: "thread-1",
    });

    expect(eventRef).toMatchObject({
      kind: "event",
      label: "Event",
      secondaryLabel: "event:evt-9",
      href: "",
      isLink: false,
    });

    const topicRef = resolveRefLink("topic:topic-1", {
      humanize: true,
    });

    expect(topicRef).toMatchObject({
      kind: "topic",
      label: "Topic topic-1",
      primaryLabel: "Topic topic-1",
      secondaryLabel: "topic:topic-1",
      href: "",
      isLink: false,
    });

    const threadRef = resolveRefLink("thread:thread-1", {
      humanize: true,
    });

    expect(threadRef).toMatchObject({
      kind: "thread",
      label: "Thread thread-1",
      primaryLabel: "Thread thread-1",
      secondaryLabel: "thread:thread-1",
      href: "",
      isLink: false,
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
      href: "",
      isLink: false,
    });
  });

  it("routes attachment artifacts to artifact detail with filename labels", () => {
    const artifactId = "aebb0220-dbfd-4fe8-bd67-eb6b317ffa43";
    const { artifactRoutesById } = buildPrimitiveRefRoutes({
      artifacts: [
        {
          id: artifactId,
          kind: "attachment",
          content_type: "application/pdf",
          original_filename: "Specs.pdf",
        },
      ],
    });

    expect(artifactRoutesById[artifactId]).toMatchObject({
      kind: "attachment",
      targetPrefix: "artifact",
      targetValue: artifactId,
      label: "Specs.pdf",
    });

    const ref = resolveRefLink(`artifact:${artifactId}`, {
      organizationSlug: "acme",
      workspaceSlug: "proj",
      humanize: true,
      artifactRoutesById,
    });

    expect(ref).toMatchObject({
      routed: true,
      routedKind: "attachment",
      routedPrefix: "artifact",
      routedValue: artifactId,
      primaryLabel: "Specs.pdf",
      secondaryLabel: "",
      href: `/o/acme/w/proj/artifacts/${artifactId}`,
      isLink: true,
    });
  });

  it("adds format hint for attachment names without file extensions", () => {
    const artifactId = "b1b2c3d4-e5f6-7890-abcd-ef1234567890";
    const { artifactRoutesById } = buildPrimitiveRefRoutes({
      artifacts: [
        {
          id: artifactId,
          kind: "attachment",
          content_type: "text/markdown",
          original_filename: "notes",
        },
      ],
    });

    expect(artifactRoutesById[artifactId].label).toBe("notes · MD");
  });

  it("accepts thread-style artifact maps keyed by id", () => {
    const aid = "aebb0220-dbfd-4fe8-bd67-eb6b317ffa43";
    const { artifactRoutesById } = buildPrimitiveRefRoutes({
      artifacts: {
        [aid]: {
          id: aid,
          kind: "attachment",
          content_type: "application/pdf",
          original_filename: "Specs.pdf",
        },
      },
    });

    expect(artifactRoutesById[aid]).toMatchObject({
      kind: "attachment",
      label: "Specs.pdf",
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
