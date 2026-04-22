import { describe, expect, it } from "vitest";

import {
  buildTopicCreatePayloadForUi,
  buildTopicCreatePayloadFromDraft,
  mapThreadStatusToTopicStatus,
} from "../../src/lib/topicCreatePayload.js";

describe("topicCreatePayload", () => {
  it("buildTopicCreatePayloadForUi includes all fields core requires on create", () => {
    const payload = buildTopicCreatePayloadForUi({ title: "Hello" });
    expect(payload.topic).toMatchObject({
      type: "other",
      status: "active",
      title: "Hello",
      summary: "No summary provided.",
      owner_refs: [],
      document_refs: [],
      board_refs: [],
      related_refs: [],
      provenance: { sources: ["actor_statement:ui"] },
    });
  });

  it("buildTopicCreatePayloadForUi uses custom summary when provided", () => {
    const payload = buildTopicCreatePayloadForUi({
      title: "T",
      summary: " Custom ",
    });
    expect(payload.topic.summary).toBe("Custom");
  });

  it("mapThreadStatusToTopicStatus maps paused and closed", () => {
    expect(mapThreadStatusToTopicStatus("paused")).toBe("blocked");
    expect(mapThreadStatusToTopicStatus("closed")).toBe("resolved");
    expect(mapThreadStatusToTopicStatus("active")).toBe("active");
  });

  it("buildTopicCreatePayloadFromDraft maps draft status and summary", () => {
    const payload = buildTopicCreatePayloadFromDraft({
      title: "X",
      summary: "",
      status: "paused",
    });
    expect(payload.topic).toMatchObject({
      type: "other",
      status: "blocked",
      title: "X",
      summary: "No summary provided.",
    });
  });
});
