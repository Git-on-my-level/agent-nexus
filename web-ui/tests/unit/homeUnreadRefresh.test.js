import { describe, expect, it } from "vitest";
import {
  createStatusChangeDecisionPayload,
  statusChangeInstruction,
} from "../../src/lib/taskBoardMove.js";

describe("workspace root redirect copy", () => {
  it("requests a source status change without silent mutation language", () => {
    expect(
      statusChangeInstruction(
        { source: { authority: "github" } },
        "in_progress",
      ),
    ).toBe("request status change at GitHub to In progress");
    expect(
      createStatusChangeDecisionPayload(
        { ref: "card:gh", source: { authority: "github" }, version: 2 },
        "review",
      ),
    ).toMatchObject({
      work_ref: "card:gh",
      scope: "work.phase",
      target_revision: "2",
    });
  });
});
