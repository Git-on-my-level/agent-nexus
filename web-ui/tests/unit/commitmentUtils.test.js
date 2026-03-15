import { describe, expect, it } from "vitest";

import {
  buildCommitmentPatch,
  validateCommitmentStatusTransition,
} from "../../src/lib/commitmentUtils.js";
import { parseListInput, serializeListInput } from "../../src/lib/typedRefs.js";

describe("commitment patch builder", () => {
  it("includes scalar changes only when modified", () => {
    const original = {
      title: "Old title",
      owner: "actor-1",
      status: "open",
      due_at: "2026-03-15T00:00:00.000Z",
    };

    const draft = {
      ...original,
      title: "New title",
      owner: "actor-2",
    };

    expect(buildCommitmentPatch(original, draft)).toEqual({
      title: "New title",
      owner: "actor-2",
    });
  });

  it("replaces list fields wholesale when changed", () => {
    const original = {
      definition_of_done: ["One"],
      links: ["thread:thread-1"],
    };
    const draft = {
      definition_of_done: ["One", "Two"],
      links: ["thread:thread-1", "artifact:artifact-1"],
    };

    expect(buildCommitmentPatch(original, draft)).toEqual({
      definition_of_done: ["One", "Two"],
      links: ["thread:thread-1", "artifact:artifact-1"],
    });
  });
});

describe("commitment transition validation", () => {
  it("allows non-restricted statuses without refs", () => {
    expect(validateCommitmentStatusTransition("open", "")).toEqual({
      valid: true,
      error: "",
    });
    expect(validateCommitmentStatusTransition("blocked", "")).toEqual({
      valid: true,
      error: "",
    });
  });

  it("requires artifact/event ref for status done", () => {
    expect(validateCommitmentStatusTransition("done", "")).toMatchObject({
      valid: false,
    });
    expect(
      validateCommitmentStatusTransition("done", "event:event-123"),
    ).toEqual({
      valid: true,
      error: "",
    });
    expect(
      validateCommitmentStatusTransition("done", "artifact:artifact-123"),
    ).toEqual({
      valid: true,
      error: "",
    });
    expect(
      validateCommitmentStatusTransition("done", "snapshot:commitment-123"),
    ).toMatchObject({
      valid: false,
    });
  });

  it("requires event ref for status canceled", () => {
    expect(validateCommitmentStatusTransition("canceled", "")).toMatchObject({
      valid: false,
    });
    expect(
      validateCommitmentStatusTransition("canceled", "event:event-123"),
    ).toEqual({
      valid: true,
      error: "",
    });
    expect(
      validateCommitmentStatusTransition("canceled", "artifact:artifact-123"),
    ).toMatchObject({
      valid: false,
    });
  });
});

describe("commitment list input helpers", () => {
  it("parses and serializes list input", () => {
    expect(parseListInput("one, two\nthree")).toEqual(["one", "two", "three"]);
    expect(serializeListInput(["one", "two"])).toBe("one\ntwo");
  });
});
