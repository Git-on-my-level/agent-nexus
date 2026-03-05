import { describe, expect, it } from "vitest";

import {
  ensureThreadRef,
  parseWorkOrderListInput,
  serializeWorkOrderListInput,
  validateTypedRefs,
  validateWorkOrderDraft,
} from "../../src/lib/workOrderUtils.js";

describe("work order list helpers", () => {
  it("parses and serializes list input", () => {
    expect(parseWorkOrderListInput("one, two\nthree")).toEqual([
      "one",
      "two",
      "three",
    ]);
    expect(serializeWorkOrderListInput(["one", "two"])).toBe("one\ntwo");
  });

  it("ensures thread ref is present", () => {
    expect(ensureThreadRef(["artifact:a"], "thread-1")).toEqual([
      "thread:thread-1",
      "artifact:a",
    ]);
    expect(
      ensureThreadRef(["thread:thread-1", "artifact:a"], "thread-1"),
    ).toEqual(["thread:thread-1", "artifact:a"]);
  });
});

describe("typed ref validation", () => {
  it("rejects malformed refs", () => {
    expect(validateTypedRefs(["artifact:a", "event:evt-1"])).toEqual({
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
      { threadId: "thread-1" },
    );

    expect(result.valid).toBe(true);
    expect(result.errors).toEqual([]);
    expect(result.normalized).toMatchObject({
      thread_id: "thread-1",
      objective: "Ship onboarding fix",
      constraints: ["No downtime"],
      context_refs: ["thread:thread-1", "artifact:artifact-1"],
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
      { threadId: "thread-1" },
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
