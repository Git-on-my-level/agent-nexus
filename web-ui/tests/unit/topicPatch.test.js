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
      summary: "Original summary",
      open_cards: ["card-1"],
    };
    const draft = {
      ...original,
      title: "Updated title",
      summary: "Updated summary",
      open_cards: ["card-2"],
    };

    expect(buildTopicPatch(original, draft)).toEqual({
      title: "Updated title",
      summary: "Updated summary",
    });
  });

  it("uses summary and ignores legacy current_summary alias", () => {
    expect(
      buildTopicPatch(
        { summary: "Old", current_summary: "Legacy old" },
        { summary: "New", current_summary: "Legacy new" },
      ),
    ).toEqual({ summary: "New" });
  });

  it("ignores removed topic fields", () => {
    const original = {
      tags: ["ops", "customer"],
      priority: "p1",
      cadence: "weekly",
      next_check_in_at: "2026-03-05T00:00:00.000Z",
      next_actions: ["Do A"],
      key_artifacts: ["artifact:a"],
    };
    const draft = {
      tags: ["ops", "customer", "legal"],
      priority: "p0",
      cadence: "daily",
      next_check_in_at: null,
      next_actions: ["Do A"],
      key_artifacts: ["artifact:b", "artifact:c"],
    };

    expect(buildTopicPatch(original, draft)).toEqual({});
  });
});

describe("thread list input helpers", () => {
  it("parses and serializes list fields", () => {
    expect(parseListInput("one, two\nthree")).toEqual(["one", "two", "three"]);
    expect(serializeListInput(["one", "two", "three"])).toBe("one\ntwo\nthree");
  });
});
