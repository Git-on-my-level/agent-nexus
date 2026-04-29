import { describe, expect, it } from "vitest";

import { validateEventCreatePayload } from "../../src/lib/eventValidation.js";

function validBaseEvent(overrides = {}) {
  return {
    actor_id: "actor-1",
    event: {
      type: "topic_created",
      summary: "hello",
      refs: ["topic:topic-1"],
      provenance: { sources: ["event:event-1"] },
      ...overrides,
    },
  };
}

describe("event validation", () => {
  it("accepts valid event payloads", () => {
    expect(validateEventCreatePayload(validBaseEvent())).toBe("");
  });

  it("allows message_posted without a thread_id requirement", () => {
    const error = validateEventCreatePayload(
      validBaseEvent({
        type: "message_posted",
        refs: ["thread:thread-1"],
        thread_id: undefined,
      }),
    );

    expect(error).toBe("");
  });

  it("accepts message_posted with thread_ref alongside thread_id", () => {
    const error = validateEventCreatePayload(
      validBaseEvent({
        type: "message_posted",
        thread_id: "thread-1",
        thread_ref: "thread:thread-1",
        refs: ["thread:thread-1"],
        payload: { text: "hello" },
      }),
    );

    expect(error).toBe("");
  });

  it("rejects card_moved payloads that miss required board refs", () => {
    const error = validateEventCreatePayload(
      validBaseEvent({
        type: "card_moved",
        refs: ["card:card_1"],
        payload: { column_key: "done" },
      }),
    );

    expect(error).toContain('event.refs must include a "board:<id>"');
  });

  it("rejects review_completed payloads that miss required card ref", () => {
    const error = validateEventCreatePayload(
      validBaseEvent({
        type: "review_completed",
        refs: ["artifact:plan_1", "artifact:receipt_1"],
        payload: { subject_ref: "card:card_1" },
      }),
    );

    expect(error).toContain('"card:<id>" typed ref');
  });

  it("keeps unknown event types open", () => {
    const error = validateEventCreatePayload(
      validBaseEvent({
        type: "future_custom_type",
        thread_id: undefined,
        refs: ["mystery:opaque"],
      }),
    );

    expect(error).toBe("");
  });

  it("requires the right refs for human attention request and response events", () => {
    expect(
      validateEventCreatePayload(
        validBaseEvent({
          type: "human_attention_requested",
          refs: ["topic:topic-1"],
          payload: {
            kind: "ask",
            title: "Need approval",
            subject_ref: "topic:topic-1",
            requester_actor_id: "actor-1",
            response_proposals: ["Approve", "Reject"],
          },
        }),
      ),
    ).toContain('event.refs must include a "thread:<id>"');

    expect(
      validateEventCreatePayload(
        validBaseEvent({
          type: "human_attention_requested",
          refs: ["thread:thread-1"],
          payload: {
            kind: "ask",
            title: "Need approval",
            subject_ref: "topic:topic-1",
            requester_actor_id: "actor-1",
            response_proposals: ["Approve", "Reject"],
          },
        }),
      ),
    ).toBe("");

    expect(
      validateEventCreatePayload(
        validBaseEvent({
          type: "human_attention_responded",
          refs: ["thread:thread-1"],
          payload: {
            inbox_item_id: "inbox-1",
            kind: "ask",
            response_text: "Approved",
            responding_actor_id: "actor-1",
          },
        }),
      ),
    ).toContain('event.refs must include a "inbox:<id>"');

    expect(
      validateEventCreatePayload(
        validBaseEvent({
          type: "human_attention_responded",
          refs: ["inbox:inbox-1"],
          payload: {
            inbox_item_id: "inbox-1",
            kind: "ask",
            response_text: "Approved",
            responding_actor_id: "actor-1",
          },
        }),
      ),
    ).toBe("");
  });
});
