import { describe, expect, it } from "vitest";

import {
  buildTopicPatch,
  parseListInput,
  serializeListInput,
} from "../../src/lib/topicPatch.js";

describe("topic patch builder", () => {
  it("includes only changed scalar fields", () => {
    const original = {
      title: "Original title",
      status: "active",
      priority: "p1",
      open_cards: ["card-1"],
    };
    const draft = {
      ...original,
      title: "Updated title",
      open_cards: ["card-2"],
    };

    expect(buildTopicPatch(original, draft)).toEqual({
      title: "Updated title",
    });
  });

  it("replaces list fields wholesale when changed and omits untouched lists", () => {
    const original = {
      tags: ["ops", "customer"],
      next_actions: ["Do A"],
      key_artifacts: ["artifact:a"],
    };
    const draft = {
      tags: ["ops", "customer", "legal"],
      next_actions: ["Do A"],
      key_artifacts: ["artifact:b", "artifact:c"],
    };

    expect(buildTopicPatch(original, draft)).toEqual({
      tags: ["ops", "customer", "legal"],
    });
  });
});

describe("thread list input helpers", () => {
  it("parses and serializes list fields", () => {
    expect(parseListInput("one, two\nthree")).toEqual(["one", "two", "three"]);
    expect(serializeListInput(["one", "two", "three"])).toBe("one\ntwo\nthree");
  });
});
