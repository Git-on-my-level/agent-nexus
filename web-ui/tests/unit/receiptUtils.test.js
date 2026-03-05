import { describe, expect, it } from "vitest";

import {
  parseReceiptListInput,
  serializeReceiptListInput,
  validateReceiptDraft,
  validateReceiptTypedRefs,
} from "../../src/lib/receiptUtils.js";

describe("receipt list helpers", () => {
  it("parses and serializes list input", () => {
    expect(parseReceiptListInput("one, two\nthree")).toEqual([
      "one",
      "two",
      "three",
    ]);
    expect(serializeReceiptListInput(["one", "two"])).toBe("one\ntwo");
  });
});

describe("receipt typed-ref validation", () => {
  it("detects malformed typed refs", () => {
    expect(validateReceiptTypedRefs(["artifact:a", "event:e-1"])).toEqual({
      valid: true,
      invalidRefs: [],
    });
    expect(validateReceiptTypedRefs(["bad", "url:"])).toEqual({
      valid: false,
      invalidRefs: ["bad", "url:"],
    });
  });
});

describe("receipt draft validation", () => {
  it("validates required fields and normalizes parsed lists", () => {
    const result = validateReceiptDraft(
      {
        workOrderId: "artifact-work-order-1",
        outputsInput: "artifact:artifact-output-1",
        verificationEvidenceInput: "artifact:artifact-test-log",
        changesSummary: "Implemented the requested flow.",
        knownGapsInput: "Need one more integration test",
      },
      { threadId: "thread-1" },
    );

    expect(result.valid).toBe(true);
    expect(result.errors).toEqual([]);
    expect(result.normalized).toMatchObject({
      thread_id: "thread-1",
      work_order_id: "artifact-work-order-1",
      outputs: ["artifact:artifact-output-1"],
      verification_evidence: ["artifact:artifact-test-log"],
      changes_summary: "Implemented the requested flow.",
      known_gaps: ["Need one more integration test"],
    });
  });

  it("returns clear errors for invalid draft", () => {
    const result = validateReceiptDraft(
      {
        workOrderId: "",
        outputsInput: "not-a-ref",
        verificationEvidenceInput: "",
        changesSummary: "",
        knownGapsInput: "",
      },
      { threadId: "thread-1" },
    );

    expect(result.valid).toBe(false);
    expect(result.errors).toEqual([
      "work_order_id is required.",
      "changes_summary is required.",
      "verification_evidence must include at least one typed ref.",
      "Invalid typed refs in outputs: not-a-ref",
    ]);
  });
});
