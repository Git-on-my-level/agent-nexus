import { describe, expect, it } from "vitest";

import {
  eventRefsInclude,
  flattenMessageThreadView,
  toFlatMessageView,
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

  it("exposes documentComment for document_text_comment payloads", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "m-1",
          ts: "2026-04-20T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "a1",
          refs: [
            "thread:thread-1",
            "document:doc-1",
            "document_revision:rev-1",
          ],
          payload: {
            text: "Fix wording",
            kind: "document_text_comment",
            document_comment: {
              document_id: "doc-1",
              revision_id: "rev-1",
              content_hash: "h1",
              selected_text: "Hello",
              anchor_status: "current",
            },
          },
        },
      ],
      { threadId: "thread-1" },
    );
    const m = threads[0];
    expect(m.messageText).toBe("Fix wording");
    expect(m.documentComment).toMatchObject({
      document_id: "doc-1",
      revision_id: "rev-1",
      content_hash: "h1",
      selected_text: "Hello",
      anchor_status: "current",
    });
  });

  it("hides redundant document and document_revision refs on doc-text comments", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "m-2",
          ts: "2026-04-20T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "a1",
          refs: [
            "thread:thread-1",
            "document:doc-1",
            "document_revision:rev-1",
            "artifact:a-1",
          ],
          payload: {
            text: "Fix wording",
            kind: "document_text_comment",
            document_comment: {
              document_id: "doc-1",
              revision_id: "rev-1",
              selected_text: "Hello",
              anchor_status: "current",
            },
          },
        },
      ],
      { threadId: "thread-1" },
    );
    const m = threads[0];
    expect(m.displayRefs).toEqual(["artifact:a-1"]);
  });

  it("keeps document refs visible on plain (non-anchored) discussion posts", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "m-3",
          ts: "2026-04-20T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "a1",
          refs: ["thread:thread-1", "document:doc-1"],
          payload: { text: "Plain discussion note" },
        },
      ],
      { threadId: "thread-1" },
    );
    const m = threads[0];
    expect(m.documentComment).toBeNull();
    expect(m.displayRefs).toEqual(["document:doc-1"]);
  });

  it("hides document + document_revision ref chips when suppressDisplayDocumentId matches", () => {
    const threads = toMessageThreadView(
      [
        {
          id: "m-reply",
          ts: "2026-04-20T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "a1",
          refs: [
            "thread:thread-1",
            "event:parent",
            "document:doc-1",
            "document_revision:rev-1",
          ],
          payload: { text: "Nested reply on same doc" },
        },
      ],
      { threadId: "thread-1", suppressDisplayDocumentId: "doc-1" },
    );
    expect(threads[0].displayRefs).toEqual([]);
  });

  it("resolves reply parents when stored refs use public event handles", () => {
    const flat = toFlatMessageView(
      [
        {
          id: "ev-root-uuid",
          handle: "message-posted-root",
          ref: "event:message-posted-root",
          ts: "2026-03-03T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "actor-a",
          refs: ["thread:general"],
          payload: { text: "root message" },
        },
        {
          id: "ev-reply-uuid",
          handle: "message-posted-reply",
          ref: "event:message-posted-reply",
          ts: "2026-03-03T10:01:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "actor-b",
          refs: ["thread:general", "event:message-posted-root"],
          payload: { text: "reply text" },
        },
      ],
      { threadId: "thread-uuid-1" },
    );

    expect(flat[1].replyTo).toMatchObject({
      id: "ev-root-uuid",
      authorActorId: "actor-a",
      text: "root message",
    });
    expect(flat[1].displayRefs).toEqual([]);
  });

  it("toFlatMessageView sorts chronologically with reply previews", () => {
    const flat = toFlatMessageView(
      [
        {
          id: "reply-1",
          ts: "2026-03-03T10:01:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "actor-b",
          refs: ["thread:general", "event:root-1"],
          payload: { text: "first reply" },
        },
        {
          id: "root-1",
          ts: "2026-03-03T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "actor-a",
          refs: ["thread:general"],
          payload: { text: "root message" },
        },
      ],
      { threadId: "thread-uuid-1" },
    );

    expect(flat.map((m) => m.id)).toEqual(["root-1", "reply-1"]);
    expect(flat[1].replyTo).toMatchObject({
      id: "root-1",
      authorActorId: "actor-a",
      text: "root message",
    });
    expect(flat[0].replyTo).toBeNull();
    expect(flat.every((m) => m.displayRefs.length === 0)).toBe(true);
  });

  it("attaches notification receipts to the triggering message", () => {
    const receipts = [
      {
        wakeup_id: "wake-1",
        target_handle: "hermes-m2",
        delivery_status: "claimed",
        notification_status: "unread",
      },
    ];
    const threads = toMessageThreadView(
      [
        {
          id: "m-receipt",
          ts: "2026-04-20T10:00:00.000Z",
          type: "message_posted",
          thread_id: "thread-1",
          actor_id: "a1",
          refs: ["thread:thread-1"],
          payload: { text: "@hermes-m2 please check this" },
        },
      ],
      {
        threadId: "thread-1",
        notificationReceiptsByEventId: {
          "m-receipt": receipts,
        },
      },
    );
    expect(threads[0].notificationReceipts).toEqual(receipts);
  });
});
