import { describe, expect, it } from "vitest";

import {
  flattenMessageThreadView,
  toMessageThreadView,
} from "../../src/lib/messageThreadUtils.js";

describe("message thread utils", () => {
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
