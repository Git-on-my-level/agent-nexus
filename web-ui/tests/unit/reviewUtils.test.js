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
    expect(parseListInput("one, two\nthree")).toEqual(["one", "two", "three"]);
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
    subjectRef: "card:card-1",
    receiptId: "artifact-receipt-1",
    reviewId: "artifact-review-1",
  };

  it("builds valid payload with evidence_refs", () => {
    const result = buildReviewPayload(
      {
        outcome: "accept",
        notes: "Looks good.",
        evidenceRefsInput: "artifact:artifact-evidence-1",
      },
      baseOptions,
    );

    expect(result.valid).toBe(true);
    expect(result.errors).toEqual([]);
    expect(result.packet).toEqual({
      review_id: "artifact-review-1",
      subject_ref: "card:card-1",
      receipt_ref: "artifact:artifact-receipt-1",
      outcome: "accept",
      notes: "Looks good.",
      evidence_refs: ["artifact:artifact-evidence-1"],
    });
    expect(result.artifact.refs).toEqual([
      "card:card-1",
      "artifact:artifact-receipt-1",
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
        subjectRef: "",
        receiptId: "",
        reviewId: "",
      },
    );

    expect(result.valid).toBe(false);
    expect(result.errors).toEqual([
      "subject_ref is required.",
      "receipt_ref or receipt_id is required.",
      "outcome must be one of: accept, revise, escalate.",
      "notes is required.",
      "Invalid typed refs in evidence_refs: bad-ref",
    ]);
  });

  it("returns null packet and artifact for invalid review payloads", () => {
    const result = buildReviewPayload(
      {
        outcome: "unknown",
        notes: "",
      },
      baseOptions,
    );

    expect(result.valid).toBe(false);
    expect(result.packet).toBeNull();
    expect(result.artifact).toBeNull();
  });
});
