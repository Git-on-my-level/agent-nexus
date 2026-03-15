import { describe, expect, it } from "vitest";

import {
  buildReviewPayload,
  validateReviewDraft,
} from "../../src/lib/reviewUtils.js";
import {
  parseListInput,
  serializeListInput,
  validateTypedRefs,
} from "../../src/lib/typedRefs.js";

describe("review list helpers", () => {
  it("parses and serializes evidence ref input", () => {
    expect(parseListInput("one, two\nthree")).toEqual([
      "one",
      "two",
      "three",
    ]);
    expect(serializeListInput(["one", "two"])).toBe("one\ntwo");
  });
});

describe("review typed-ref validation", () => {
  it("allows empty refs and rejects malformed refs", () => {
    expect(validateTypedRefs([])).toEqual({
      valid: true,
      invalidRefs: [],
    });
    expect(validateTypedRefs(["artifact:a", "event:e-1"])).toEqual({
      valid: true,
      invalidRefs: [],
    });
    expect(validateTypedRefs(["bad"])).toEqual({
      valid: false,
      invalidRefs: ["bad"],
    });
  });
});

describe("review draft/payload builder", () => {
  const baseOptions = {
    threadId: "thread-1",
    receiptId: "artifact-receipt-1",
    workOrderId: "artifact-work-order-1",
    reviewId: "artifact-review-1",
  };

  it("builds valid payload and keeps evidence_refs optional", () => {
    const result = buildReviewPayload(
      {
        outcome: "accept",
        notes: "Looks good.",
        evidenceRefsInput: "",
      },
      baseOptions,
    );

    expect(result.valid).toBe(true);
    expect(result.errors).toEqual([]);
    expect(result.packet).toEqual({
      review_id: "artifact-review-1",
      work_order_id: "artifact-work-order-1",
      receipt_id: "artifact-receipt-1",
      outcome: "accept",
      notes: "Looks good.",
      evidence_refs: [],
    });
    expect(result.artifact.refs).toEqual([
      "thread:thread-1",
      "artifact:artifact-receipt-1",
      "artifact:artifact-work-order-1",
    ]);
  });

  it("returns schema-like validation errors for invalid inputs", () => {
    const result = validateReviewDraft(
      {
        outcome: "unknown",
        notes: "",
        evidenceRefsInput: "bad-ref",
      },
      {
        threadId: "",
        receiptId: "",
        workOrderId: "",
        reviewId: "",
      },
    );

    expect(result.valid).toBe(false);
    expect(result.errors).toEqual([
      "thread_id is required.",
      "receipt_id is required.",
      "work_order_id is required.",
      "review_id is required.",
      "outcome must be one of: accept, revise, escalate.",
      "notes is required.",
      "Invalid typed refs in evidence_refs: bad-ref",
    ]);
  });
});
