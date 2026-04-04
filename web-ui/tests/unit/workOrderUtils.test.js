import { describe, expect, it } from "vitest";

import {
  applyWorkOrderContextPrefill,
  buildWorkOrderContextSuggestions,
  ensureSubjectAnchorRef,
  mergeContextRefsInput,
  removeContextRefsFromInput,
  validateWorkOrderDraft,
} from "../../src/lib/workOrderUtils.js";
import {
  parseListInput,
  serializeListInput,
  validateTypedRefs,
} from "../../src/lib/typedRefs.js";

describe("work order list helpers", () => {
  it("parses and serializes list input", () => {
    expect(parseListInput("one, two\nthree")).toEqual(["one", "two", "three"]);
    expect(serializeListInput(["one", "two"])).toBe("one\ntwo");
  });

  it("ensures subject anchor ref is present first", () => {
    expect(ensureSubjectAnchorRef(["artifact:a"], "topic:thread-1")).toEqual([
      "topic:thread-1",
      "artifact:a",
    ]);
    expect(
      ensureSubjectAnchorRef(
        ["topic:thread-1", "artifact:a"],
        "topic:thread-1",
      ),
    ).toEqual(["topic:thread-1", "artifact:a"]);
  });
});

describe("typed ref validation", () => {
  it("rejects malformed refs", () => {
    expect(
      validateTypedRefs([
        "artifact:a",
        "event:evt-1",
        "document:doc-1",
        "document_revision:rev-1",
      ]),
    ).toEqual({
      valid: true,
      invalidRefs: [],
    });
    expect(validateTypedRefs(["badref", "url:"])).toEqual({
      valid: false,
      invalidRefs: ["badref", "url:"],
    });
  });
});

describe("work order draft validation", () => {
  it("validates required fields and returns normalized payload fields", () => {
    const result = validateWorkOrderDraft(
      {
        objective: "Ship onboarding fix",
        constraintsInput: "No downtime",
        contextRefsInput: "artifact:artifact-1",
        acceptanceCriteriaInput: "All tests pass",
        definitionOfDoneInput: "Merged to main",
      },
      { subjectRef: "topic:thread-1" },
    );

    expect(result.valid).toBe(true);
    expect(result.errors).toEqual([]);
    expect(result.normalized).toMatchObject({
      subject_ref: "topic:thread-1",
      objective: "Ship onboarding fix",
      constraints: ["No downtime"],
      context_refs: ["topic:thread-1", "artifact:artifact-1"],
      acceptance_criteria: ["All tests pass"],
      definition_of_done: ["Merged to main"],
    });
  });

  it("returns clear errors for invalid draft", () => {
    const result = validateWorkOrderDraft(
      {
        objective: "",
        constraintsInput: "",
        contextRefsInput: "not-a-typed-ref",
        acceptanceCriteriaInput: "",
        definitionOfDoneInput: "",
      },
      { subjectRef: "topic:thread-1" },
    );

    expect(result.valid).toBe(false);
    expect(result.errors).toEqual([
      "Objective is required.",
      "At least one constraint is required.",
      "At least one acceptance criterion is required.",
      "At least one definition-of-done item is required.",
      "Invalid typed refs in context_refs: not-a-typed-ref",
    ]);
  });
});

describe("work order context suggestions", () => {
  it("builds deduped suggestions from key artifacts, recent events, and docs", () => {
    const suggestions = buildWorkOrderContextSuggestions({
      threadId: "thread-1",
      snapshot: {
        key_artifacts: ["artifact:artifact-1", "artifact-1"],
      },
      documents: [
        {
          id: "doc-1",
          title: "Runbook",
          status: "active",
          head_revision: { revision_number: 2, content_type: "text" },
        },
      ],
      timeline: [
        {
          id: "evt-2",
          type: "decision_made",
          ts: "2026-03-08T02:00:00Z",
          summary: "Approve launch",
        },
        {
          id: "evt-1",
          type: "receipt_added",
          ts: "2026-03-08T01:00:00Z",
          summary: "Receipt posted",
          payload: { artifact_id: "artifact-2" },
        },
        {
          id: "evt-3",
          type: "review_completed",
          ts: "2026-03-08T00:00:00Z",
          summary: "Review completed",
          payload: { artifact_id: "artifact-3" },
        },
      ],
    });

    expect(suggestions).toEqual([
      expect.objectContaining({
        ref: "artifact:artifact-1",
        source: "Key artifact",
      }),
      expect.objectContaining({
        ref: "event:evt-2",
        source: "Recent decision",
      }),
      expect.objectContaining({
        ref: "artifact:artifact-2",
        source: "Recent receipt",
      }),
      expect.objectContaining({
        ref: "artifact:artifact-3",
        source: "Recent review",
      }),
      expect.objectContaining({
        ref: "document:doc-1",
        source: "Linked document",
        detail: "active • v2 • text",
      }),
    ]);
  });
});

describe("context ref input merging", () => {
  it("dedupes merged refs and keeps the subject ref first", () => {
    expect(
      mergeContextRefsInput(
        "topic:thread-1\nartifact:artifact-1",
        ["artifact:artifact-1", "document:doc-1", "url:https://example.com"],
        { subjectRef: "topic:thread-1" },
      ),
    ).toBe(
      "topic:thread-1\nartifact:artifact-1\ndocument:doc-1\nurl:https://example.com",
    );
  });

  it("removes only selected suggestion refs and preserves manual entries", () => {
    expect(
      removeContextRefsFromInput(
        "topic:thread-1\nartifact:artifact-1\nurl:https://example.com\ndocument:doc-1",
        ["artifact:artifact-1", "document:doc-1"],
        { subjectRef: "topic:thread-1" },
      ),
    ).toBe("topic:thread-1\nurl:https://example.com");
  });

  it("applies prefill once and does not reapply after manual edits", () => {
    const first = applyWorkOrderContextPrefill({
      currentInput: "topic:thread-1",
      subjectRef: "topic:thread-1",
      prefillRefs: ["artifact:artifact-1", "artifact:artifact-1"],
      prefillKey: "thread-1|artifact:artifact-1",
      appliedPrefillKey: "",
    });

    expect(first).toEqual({
      applied: true,
      nextInput: "topic:thread-1\nartifact:artifact-1",
      nextAppliedPrefillKey: "thread-1|artifact:artifact-1",
    });

    const second = applyWorkOrderContextPrefill({
      currentInput: "topic:thread-1",
      subjectRef: "topic:thread-1",
      prefillRefs: ["artifact:artifact-1"],
      prefillKey: "thread-1|artifact:artifact-1",
      appliedPrefillKey: first.nextAppliedPrefillKey,
    });

    expect(second).toEqual({
      applied: false,
      nextInput: "topic:thread-1",
      nextAppliedPrefillKey: "thread-1|artifact:artifact-1",
    });
  });
});
