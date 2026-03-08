import { describe, it, expect } from "vitest";
import {
  getEventRefRule,
  hasEventRefRule,
  validateEventRefRule,
  validateCommitmentStatusRef,
} from "../../src/lib/eventRefRules.js";

describe("eventRefRules", () => {
  describe("getEventRefRule", () => {
    it("returns rule for known event type", () => {
      const rule = getEventRefRule("commitment_status_changed");
      expect(rule).toBeTruthy();
      expect(rule.thread_id).toBe("required");
      expect(rule.conditional_refs).toHaveLength(2);
    });

    it("returns null for unknown event type", () => {
      const rule = getEventRefRule("unknown_event");
      expect(rule).toBeNull();
    });
  });

  describe("hasEventRefRule", () => {
    it("returns true for known event type", () => {
      expect(hasEventRefRule("commitment_status_changed")).toBe(true);
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

    it("rejects missing thread_id when required", () => {
      const result = validateEventRefRule(
        "commitment_status_changed",
        ["snapshot:commitment-1"],
        { to_status: "done" },
      );
      expect(result.valid).toBe(false);
      expect(result.error).toContain("thread_id is required");
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

    it("enforces payload_must_include contract fields", () => {
      const missingSubtype = validateEventRefRule(
        "exception_raised",
        ["thread:thread-1"],
        { thread_id: "thread-1" },
      );
      expect(missingSubtype.valid).toBe(false);
      expect(missingSubtype.error).toContain(
        "event.payload.subtype is required",
      );

      const withSubtype = validateEventRefRule(
        "exception_raised",
        ["thread:thread-1"],
        { thread_id: "thread-1", subtype: "stale_thread" },
      );
      expect(withSubtype.valid).toBe(true);
    });

    it("rejects missing conditional refs for done status", () => {
      const result = validateEventRefRule(
        "commitment_status_changed",
        ["snapshot:commitment-1"],
        { thread_id: "thread-1", to_status: "done" },
      );
      expect(result.valid).toBe(false);
      expect(result.error).toContain('payload.to_status="done"');
    });

    it("allows artifact ref for done status", () => {
      const result = validateEventRefRule(
        "commitment_status_changed",
        ["snapshot:commitment-1", "artifact:receipt-1"],
        { thread_id: "thread-1", to_status: "done" },
      );
      expect(result.valid).toBe(true);
    });

    it("allows event ref for done status", () => {
      const result = validateEventRefRule(
        "commitment_status_changed",
        ["snapshot:commitment-1", "event:decision-1"],
        { thread_id: "thread-1", to_status: "done" },
      );
      expect(result.valid).toBe(true);
    });

    it("requires event ref for canceled status", () => {
      const result = validateEventRefRule(
        "commitment_status_changed",
        ["snapshot:commitment-1", "artifact:receipt-1"],
        { thread_id: "thread-1", to_status: "canceled" },
      );
      expect(result.valid).toBe(false);
      expect(result.error).toContain('payload.to_status="canceled"');
    });

    it("allows event ref for canceled status", () => {
      const result = validateEventRefRule(
        "commitment_status_changed",
        ["snapshot:commitment-1", "event:decision-1"],
        { thread_id: "thread-1", to_status: "canceled" },
      );
      expect(result.valid).toBe(true);
    });
  });

  describe("validateCommitmentStatusRef", () => {
    it("allows non-restricted statuses without ref", () => {
      const result = validateCommitmentStatusRef("open", "");
      expect(result.valid).toBe(true);
    });

    it("rejects done without ref", () => {
      const result = validateCommitmentStatusRef("done", "");
      expect(result.valid).toBe(false);
      expect(result.error).toContain("artifact");
    });

    it("rejects canceled without ref", () => {
      const result = validateCommitmentStatusRef("canceled", "");
      expect(result.valid).toBe(false);
      expect(result.error).toContain("event");
    });

    it("allows artifact ref for done", () => {
      const result = validateCommitmentStatusRef("done", "artifact:receipt-1");
      expect(result.valid).toBe(true);
    });

    it("allows event ref for done", () => {
      const result = validateCommitmentStatusRef("done", "event:decision-1");
      expect(result.valid).toBe(true);
    });

    it("rejects other prefixes for done", () => {
      const result = validateCommitmentStatusRef("done", "snapshot:snap-1");
      expect(result.valid).toBe(false);
    });

    it("allows event ref for canceled", () => {
      const result = validateCommitmentStatusRef(
        "canceled",
        "event:decision-1",
      );
      expect(result.valid).toBe(true);
    });

    it("rejects artifact ref for canceled", () => {
      const result = validateCommitmentStatusRef(
        "canceled",
        "artifact:receipt-1",
      );
      expect(result.valid).toBe(false);
    });

    it("rejects invalid typed ref format", () => {
      const result = validateCommitmentStatusRef("done", "invalid-ref");
      expect(result.valid).toBe(false);
      expect(result.error).toContain("valid typed ref");
    });
  });
});
