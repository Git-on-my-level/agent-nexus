import { describe, expect, it } from "vitest";

import { buildTopicCreatePayloadFromDraft } from "../../src/lib/topicCreatePayload.js";

describe("topicCreatePayload", () => {
  it("buildTopicCreatePayloadFromDraft fills summary default", () => {
    const payload = buildTopicCreatePayloadFromDraft({
      title: "X",
      summary: "",
    });
    expect(payload.topic).toMatchObject({
      title: "X",
      summary: "No summary provided.",
    });
    expect(payload.topic.type).toBeUndefined();
    expect(payload.topic.status).toBeUndefined();
    expect(payload.topic.state).toBeUndefined();
  });

  it("buildTopicCreatePayloadFromDraft ignores legacy type on draft", () => {
    const payload = buildTopicCreatePayloadFromDraft({
      title: "T",
      summary: "S",
      type: "incident",
    });
    expect(payload.topic.type).toBeUndefined();
  });
});
