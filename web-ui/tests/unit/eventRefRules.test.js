import { describe, it, expect } from "vitest";
import {
  getEventRefRule,
  getPayloadStringAtPath,
  hasEventRefRule,
  validateEventRefRule,
} from "../../src/lib/eventRefRules.js";

describe("eventRefRules", () => {
  describe("getEventRefRule", () => {
    it("returns rule for known event type", () => {
      const rule = getEventRefRule("topic_created");
      expect(rule).toBeTruthy();
      expect(rule.refs_must_include).toEqual(["topic:<topic_handle>"]);
    });

    it("returns null for unknown event type", () => {
      const rule = getEventRefRule("unknown_event");
      expect(rule).toBeNull();
    });
  });

  describe("hasEventRefRule", () => {
    it("returns true for known event type", () => {
      expect(hasEventRefRule("card_moved")).toBe(true);
    });

    it("returns false for unknown event type", () => {
      expect(hasEventRefRule("unknown_event")).toBe(false);
    });
  });

  describe("validateEventRefRule", () => {
    it("allows unknown event types without validation", () => {
      const result = validateEventRefRule("unknown_event", [], {});
      expect(result.valid).toBe(true);
    });

    it("rejects missing topic refs when required", () => {
      const result = validateEventRefRule("topic_created", [], {});
      expect(result.valid).toBe(false);
      expect(result.error).toContain("topic:<id>");
    });

    it("accepts message_posted without an explicit thread_id requirement", () => {
      const result = validateEventRefRule(
        "message_posted",
        ["thread:thread-1"],
        {
          summary: "hello",
        },
      );
      expect(result.valid).toBe(true);
      expect(result.error).toBe("");
    });

    it("rejects non-array refs input", () => {
      const result = validateEventRefRule("message_posted", "thread:thread-1", {
        thread_id: "thread-1",
      });
      expect(result.valid).toBe(false);
      expect(result.error).toContain("must be an array");
    });

    it("rejects invalid typed ref entries", () => {
      const result = validateEventRefRule(
        "message_posted",
        ["thread:thread-1", "bad-ref"],
        { thread_id: "thread-1" },
      );
      expect(result.valid).toBe(false);
      expect(result.error).toContain("valid typed refs");
    });

    it("requires board refs for card_moved", () => {
      const bad = validateEventRefRule("card_moved", ["card:card-1"], {
        column_key: "done",
      });
      expect(bad.valid).toBe(false);
      expect(bad.error).toContain("board:<id>");
    });

    it("accepts card_resolved with required refs and payload", () => {
      const result = validateEventRefRule(
        "card_resolved",
        ["card:card-1", "board:board-1"],
        { resolution: "completed" },
      );
      expect(result.valid).toBe(true);
    });

    it("does not apply removed packet rules", () => {
      const result = validateEventRefRule(
        "review_completed",
        ["artifact:review-1", "artifact:receipt-1"],
        { subject_ref: "card:card-1" },
      );
      expect(result.valid).toBe(true);
      expect(result.error).toBe("");
    });

    it("requires the right refs for human attention request and response events", () => {
      const requestPayload = {
        kind: "ask",
        title: "Need approval",
        subject_ref: "topic:topic-1",
        requester_actor_id: "actor-1",
        response_proposals: ["Approve", "Reject"],
      };

      const requestMissingThread = validateEventRefRule(
        "human_attention_requested",
        ["topic:topic-1"],
        requestPayload,
      );
      expect(requestMissingThread.valid).toBe(false);
      expect(requestMissingThread.error).toContain("thread:<id>");

      const requestGood = validateEventRefRule(
        "human_attention_requested",
        ["thread:thread-1"],
        requestPayload,
      );
      expect(requestGood.valid).toBe(true);

      const responsePayload = {
        inbox_item_id: "inbox-1",
        kind: "ask",
        response_text: "Approved",
        responding_actor_id: "actor-1",
      };

      const responseMissingInbox = validateEventRefRule(
        "human_attention_responded",
        ["thread:thread-1"],
        responsePayload,
      );
      expect(responseMissingInbox.valid).toBe(false);
      expect(responseMissingInbox.error).toContain("inbox:<id>");

      const responseGood = validateEventRefRule(
        "human_attention_responded",
        ["inbox:inbox-1"],
        responsePayload,
      );
      expect(responseGood.valid).toBe(true);
    });
  });

  describe("getPayloadStringAtPath", () => {
    it("reads dotted paths like core getPayloadValue", () => {
      expect(
        getPayloadStringAtPath({ outer: { inner: "expected" } }, "outer.inner"),
      ).toBe("expected");
    });

    it("returns empty string for missing or non-string leaves", () => {
      expect(getPayloadStringAtPath({}, "a.b")).toBe("");
      expect(getPayloadStringAtPath({ a: { b: 1 } }, "a.b")).toBe("");
    });
  });
});
