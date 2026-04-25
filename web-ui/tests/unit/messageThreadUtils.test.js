import { describe, expect, it } from "vitest";

import {
  eventRefsInclude,
  flattenMessageThreadView,
  toMessageThreadView,
} from "../../src/lib/messageThreadUtils.js";

describe("message thread utils", () => {
  it("eventRefsInclude matches exact ref strings with trim", () => {
    expect(
      eventRefsInclude(
        { refs: ["thread:t1", "document:doc-1", "  "] },
        "document:doc-1",
      ),
    ).toBe(true);
    expect(
      eventRefsInclude(
        { refs: ["thread:t1", " document:doc-1 "] },
        "document:doc-1",
      ),
    ).toBe(true);
    expect(
      eventRefsInclude({ refs: ["document:other"] }, "document:doc-1"),
    ).toBe(false);
    expect(eventRefsInclude({ refs: [] }, "document:doc-1")).toBe(false);
    expect(eventRefsInclude({ refs: ["document:doc-1"] }, "")).toBe(true);
  });

  it("groups replies under their parent and keeps children chronological", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "reply-2",
          ts: "2026-03-03T10:02:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1", "event:root-1"],
          summary: "Message: second reply",
          payload: { text: "second reply" },
        },
        {
          id: "root-1",
          ts: "2026-03-03T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1"],
          summary: "Message: root message",
          payload: { text: "root message" },
        },
        {
          id: "reply-1",
          ts: "2026-03-03T10:01:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1", "event:root-1"],
          summary: "Message: first reply",
          payload: { text: "first reply" },
        },
      ],
      { threadId: "thread-1" },
    );

    expect(threads).toHaveLength(1);
    expect(threads[0].id).toBe("root-1");
    expect(threads[0].messageText).toBe("root message");
    expect(threads[0].children.map((child) => child.id)).toEqual([
      "reply-1",
      "reply-2",
    ]);
    expect(threads[0].children.map((child) => child.messageText)).toEqual([
      "first reply",
      "second reply",
    ]);
  });

  it("picks the parent event ref that points to another message when multiple event refs exist", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "root-1",
          ts: "2026-03-03T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1"],
          summary: "Message: root",
        },
        {
          id: "reply-1",
          ts: "2026-03-03T10:01:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1", "event:unrelated-event", "event:root-1"],
          summary: "Message: reply",
        },
      ],
      { threadId: "thread-1" },
    );

    expect(threads).toHaveLength(1);
    expect(threads[0].id).toBe("root-1");
    expect(threads[0].children.map((c) => c.id)).toEqual(["reply-1"]);
  });

  it("keeps orphan replies as top-level messages and strips structural refs", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "orphan",
          ts: "2026-03-03T10:05:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1", "event:missing-parent", "artifact:a-1"],
          summary: "Message: orphan reply",
        },
      ],
      { threadId: "thread-1" },
    );

    expect(threads).toHaveLength(1);
    expect(threads[0].id).toBe("orphan");
    expect(threads[0].displayRefs).toEqual(["artifact:a-1"]);
  });

  it("breaks mutual reply-parent cycles so both messages stay renderable as roots", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "a",
          ts: "2026-03-03T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1", "event:b"],
          summary: "Message: a",
        },
        {
          id: "b",
          ts: "2026-03-03T10:01:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1", "event:a"],
          summary: "Message: b",
        },
      ],
      { threadId: "thread-1" },
    );

    const ids = threads.map((t) => t.id).sort();
    expect(ids).toEqual(["a", "b"]);
    expect(threads.every((t) => t.children.length === 0)).toBe(true);
  });

  it("flattens threaded messages for lookup helpers", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "root-1",
          ts: "2026-03-03T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1"],
          summary: "Message: root message",
        },
        {
          id: "reply-1",
          ts: "2026-03-03T10:01:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          refs: ["thread:thread-1", "event:root-1"],
          summary: "Message: first reply",
        },
      ],
      { threadId: "thread-1" },
    );

    expect(
      flattenMessageThreadView(threads).map((message) => message.id),
    ).toEqual(["root-1", "reply-1"]);
  });
});
