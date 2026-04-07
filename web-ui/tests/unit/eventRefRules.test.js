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
      expect(rule.refs_must_include).toEqual(["topic:<topic_id>"]);
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

    it("rejects missing topic_status_changed payload fields", () => {
      const result = validateEventRefRule(
        "topic_status_changed",
        ["topic:topic-1"],
        { from_status: "active" },
      );
      expect(result.valid).toBe(false);
      expect(result.error).toContain("event.payload.to_status");
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

    it("requires card ref for review_completed", () => {
      const result = validateEventRefRule(
        "review_completed",
        ["artifact:review-1", "artifact:receipt-1"],
        { subject_ref: "card:card-1" },
      );
      expect(result.valid).toBe(false);
      expect(result.error).toContain('"card:<id>" typed ref');
    });

    it("accepts review_completed with all required refs", () => {
      const result = validateEventRefRule(
        "review_completed",
        ["artifact:review-1", "artifact:receipt-1", "card:card-1"],
        { subject_ref: "card:card-1" },
      );
      expect(result.valid).toBe(true);
    });

    it("requires thread refs for decision events", () => {
      const bad = validateEventRefRule("decision_made", ["topic:topic-1"], {});
      expect(bad.valid).toBe(false);
      expect(bad.error).toContain("thread:<id>");

      const good = validateEventRefRule(
        "decision_made",
        ["thread:thread-1"],
        {},
      );
      expect(good.valid).toBe(true);
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
