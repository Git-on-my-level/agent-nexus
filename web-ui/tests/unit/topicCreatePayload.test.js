import { describe, expect, it } from "vitest";

import {
  buildTopicCreatePayloadFromDraft,
  mapThreadStatusToTopicStatus,
} from "../../src/lib/topicCreatePayload.js";

describe("topicCreatePayload", () => {
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
