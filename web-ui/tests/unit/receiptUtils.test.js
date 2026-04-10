import { describe, expect, it } from "vitest";

import {
  buildReceiptPayload,
  validateReceiptDraft,
} from "../../src/lib/receiptUtils.js";
import {
  parseListInput,
  serializeListInput,
  validateTypedRefs,
} from "../../src/lib/typedRefs.js";

describe("receipt list helpers", () => {
  it("parses and serializes list input", () => {
    expect(parseListInput("one, two\nthree")).toEqual(["one", "two", "three"]);
    expect(serializeListInput(["one", "two"])).toBe("one\ntwo");
  });
});

describe("receipt typed-ref validation", () => {
  it("detects malformed typed refs", () => {
    expect(validateTypedRefs(["artifact:a", "event:e-1"])).toEqual({
      valid: true,
      invalidRefs: [],
    });
    expect(validateTypedRefs(["bad", "url:"])).toEqual({
      valid: false,
      invalidRefs: ["bad", "url:"],
    });
  });
});

describe("receipt draft validation", () => {
  it("validates required fields and normalizes parsed lists", () => {
    const result = validateReceiptDraft(
      {
        outputsInput: "artifact:artifact-output-1",
        verificationEvidenceInput: "artifact:artifact-test-log",
        changesSummary: "Implemented the requested flow.",
        knownGapsInput: "Need one more integration test",
      },
      { subjectRef: "card:card-1" },
    );

    expect(result.valid).toBe(true);
    expect(result.errors).toEqual([]);
    expect(result.normalized).toMatchObject({
      subject_ref: "card:card-1",
      outputs: ["artifact:artifact-output-1"],
      verification_evidence: ["artifact:artifact-test-log"],
      changes_summary: "Implemented the requested flow.",
      known_gaps: ["Need one more integration test"],
    });
  });

  it("returns clear errors for invalid draft", () => {
    const result = validateReceiptDraft(
      {
        outputsInput: "not-a-ref",
        verificationEvidenceInput: "",
        changesSummary: "",
        knownGapsInput: "",
      },
      { subjectRef: "topic:example-invalid-subject" },
    );

    expect(result.valid).toBe(false);
    expect(result.errors).toEqual([
      "subject_ref must be a card ref (card:...).",
      "changes_summary is required.",
      "verification_evidence must include at least one typed ref.",
      "Invalid typed refs in outputs: not-a-ref",
    ]);
  });

  it("builds a receipt packet and artifact with a stable result shape", () => {
    const result = buildReceiptPayload(
      {
        outputsInput: "artifact:artifact-output-1",
        verificationEvidenceInput: "artifact:artifact-test-log",
        changesSummary: "Implemented the requested flow.",
        knownGapsInput: "Need one more integration test",
      },
      {
        subjectRef: "card:card-1",
        receiptId: "artifact-receipt-1",
      },
    );

    expect(result.valid).toBe(true);
    expect(result.packet).toEqual({
      receipt_id: "artifact-receipt-1",
      subject_ref: "card:card-1",
      outputs: ["artifact:artifact-output-1"],
      verification_evidence: ["artifact:artifact-test-log"],
      changes_summary: "Implemented the requested flow.",
      known_gaps: ["Need one more integration test"],
    });
    expect(result.artifact).toEqual({
      id: "artifact-receipt-1",
      kind: "receipt",
      summary: "Implemented the requested flow.",
      refs: ["card:card-1"],
    });
  });

  it("returns null packet and artifact for invalid receipt drafts", () => {
    const result = buildReceiptPayload(
      {
        outputsInput: "not-a-ref",
        verificationEvidenceInput: "",
        changesSummary: "",
      },
      { subjectRef: "card:card-1" },
    );

    expect(result.valid).toBe(false);
    expect(result.packet).toBeNull();
    expect(result.artifact).toBeNull();
  });
});
