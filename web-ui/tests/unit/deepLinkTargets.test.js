import { describe, expect, it } from "vitest";

import {
  messageEventHref,
  messageEventHrefFromEvent,
  messageTargetFromHash,
  parseDeepLinkHash,
  threadTimelineEventHref,
  timelineTargetFromHash,
} from "../../src/lib/deepLinkTargets.js";

describe("deep link targets", () => {
  const workspaceHref = (path) => `/w${path}`;

  it("parses canonical message and timeline fragments", () => {
    expect(parseDeepLinkHash("#message-evt-1")).toEqual({
      kind: "message",
      id: "evt-1",
      legacy: false,
    });
    expect(parseDeepLinkHash("#event-evt-2")).toEqual({
      kind: "event",
      id: "evt-2",
      legacy: false,
    });
  });

  it("treats legacy bare event hashes as timeline events", () => {
    expect(parseDeepLinkHash("#evt-3")).toEqual({
      kind: "event",
      id: "evt-3",
      legacy: true,
    });
  });

  it("maps event hashes to message targets for the Messages tab", () => {
    expect(messageTargetFromHash("#message-evt-1")).toMatchObject({
      kind: "message",
      id: "evt-1",
      legacy: false,
    });
    expect(messageTargetFromHash("#event-evt-1")).toMatchObject({
      kind: "message",
      id: "evt-1",
      legacy: true,
    });
  });

  it("ignores message hashes for the Timeline tab", () => {
    expect(timelineTargetFromHash("#message-evt-1")).toEqual({
      kind: "",
      id: "",
      legacy: false,
    });
    expect(timelineTargetFromHash("#event-evt-1")).toMatchObject({
      kind: "event",
      id: "evt-1",
    });
  });

  it("builds canonical message and thread timeline hrefs", () => {
    expect(
      messageEventHref({
        workspaceHref,
        eventId: "evt-1",
        topicId: "topic-1",
        threadId: "thread-1",
      }),
    ).toBe("/w/topics/topic-1?tab=messages#message-evt-1");

    expect(
      messageEventHref({
        workspaceHref,
        eventId: "evt-1",
        threadId: "thread-1",
      }),
    ).toBe("/w/threads/thread-1?tab=messages#message-evt-1");

    expect(
      threadTimelineEventHref({
        workspaceHref,
        eventId: "evt-2",
        threadId: "thread-1",
      }),
    ).toBe("/w/threads/thread-1?tab=timeline#event-evt-2");
  });

  it("derives canonical message hrefs from event metadata", () => {
    expect(
      messageEventHrefFromEvent(
        {
          id: "evt-1",
          type: "message_posted",
          thread_ref: "thread:thread-1",
          refs: ["topic:topic-1"],
        },
        { workspaceHref },
      ),
    ).toBe("/w/topics/topic-1?tab=messages#message-evt-1");
  });
});
