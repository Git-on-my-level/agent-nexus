import { describe, expect, it } from "vitest";

import { boardDisplayTitle } from "../../src/lib/boardUtils.js";
import { handleRestatesAgentName } from "../../src/lib/agentPresence.js";
import {
  threadRowContextParts,
  threadSubjectKindLabel,
  threadSubjectRef,
} from "../../src/lib/threadListContext.js";

describe("boardDisplayTitle", () => {
  it("drops the suffix the column header already said", () => {
    expect(boardDisplayTitle("Launch Readiness Board")).toBe(
      "Launch Readiness",
    );
    expect(boardDisplayTitle("Creative / Content Board")).toBe(
      "Creative / Content",
    );
    expect(boardDisplayTitle("creative board")).toBe("creative");
  });

  it("leaves a title that is only the word, and anything else", () => {
    expect(boardDisplayTitle("Board")).toBe("Board");
    expect(boardDisplayTitle("Boardwalk")).toBe("Boardwalk");
    expect(boardDisplayTitle("Launch Boards")).toBe("Launch Boards");
    expect(boardDisplayTitle("")).toBe("");
    expect(boardDisplayTitle(null)).toBe("");
  });
});

describe("handleRestatesAgentName", () => {
  it("is true when the handle is the name with other punctuation", () => {
    expect(handleRestatesAgentName("priya on dev-host", "priya.dev-host")).toBe(
      true,
    );
    expect(
      handleRestatesAgentName("codex on workstation-a", "codex.workstation-a"),
    ).toBe(true);
  });

  it("is false when the handle says something new", () => {
    expect(handleRestatesAgentName("priya on dev-host", "legacy.bot")).toBe(
      false,
    );
    expect(handleRestatesAgentName("Priya", "priya.dev-host")).toBe(false);
    expect(handleRestatesAgentName("", "priya.dev-host")).toBe(false);
    expect(handleRestatesAgentName("priya on dev-host", "")).toBe(false);
  });
});

describe("thread row context", () => {
  const asksForMaya = {
    title: "Asks",
    subject_ref: "board:asks-maya",
    updated_by: "actor-gds-producer",
  };

  it("names what the thread belongs to", () => {
    expect(threadSubjectKindLabel(asksForMaya)).toBe("Group");
    expect(threadSubjectKindLabel({ subject_ref: "card:gds-launch-1" })).toBe(
      "Initiative",
    );
    expect(threadSubjectKindLabel({ topic_ref: "topic:launch" })).toBe(
      "Project",
    );
    expect(threadSubjectKindLabel({ subject_ref: "mystery:1" })).toBe("");
    expect(threadSubjectKindLabel({})).toBe("");
  });

  it("falls back to the legacy topic ref for the title", () => {
    expect(threadSubjectRef({ topic_ref: "topic:launch" })).toBe(
      "topic:launch",
    );
    expect(threadSubjectRef({})).toBe("");
  });

  it("tells two threads of the same name apart by their owner", () => {
    expect(
      threadRowContextParts(asksForMaya, { ownerName: "Maya Chen" }),
    ).toEqual(["Group", "Maya Chen"]);
    expect(
      threadRowContextParts(
        { ...asksForMaya, subject_ref: "board:asks-priya" },
        { ownerName: "priya" },
      ),
    ).toEqual(["Group", "priya"]);
  });

  it("drops the parts it does not have", () => {
    expect(threadRowContextParts(asksForMaya)).toEqual(["Group"]);
    expect(threadRowContextParts({}, { ownerName: "" })).toEqual([]);
  });
});
