import { describe, expect, it } from "vitest";

import { validateEventCreatePayload } from "../../src/lib/eventValidation.js";

function validBaseEvent(overrides = {}) {
  return {
    actor_id: "actor-1",
    event: {
      type: "topic_created",
      summary: "hello",
      refs: ["topic:topic-1"],
      provenance: { sources: ["actor_statement:event-1"] },
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

  it("enforces topic status transition payload refs", () => {
    const error = validateEventCreatePayload(
      validBaseEvent({
        type: "topic_status_changed",
        refs: ["topic:topic_1"],
        payload: { from_status: "active" },
      }),
    );

    expect(error).toContain("event.payload.to_status is required");
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

  it("requires thread refs for decision lifecycle events", () => {
    for (const type of [
      "decision_made",
      "decision_needed",
      "intervention_needed",
    ]) {
      expect(
        validateEventCreatePayload(
          validBaseEvent({
            type,
            thread_id: "thread-1",
            refs: ["topic:topic-1"],
          }),
        ),
      ).toContain('event.refs must include a "thread:<id>"');
    }

    expect(
      validateEventCreatePayload(
        validBaseEvent({
          type: "decision_made",
          thread_id: "thread-1",
          refs: ["thread:thread-1", "topic:topic-1"],
        }),
      ),
    ).toBe("");
  });
});
