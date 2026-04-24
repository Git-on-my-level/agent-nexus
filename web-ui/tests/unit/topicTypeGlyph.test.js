import { describe, expect, it } from "vitest";

import {
  normalizeTopicType,
  topicTypeColorVarName,
  topicTypeSelectOptions,
} from "../../src/lib/topicTypeGlyph.js";

describe("topicTypeGlyph", () => {
  it("normalizes known kinds", () => {
    expect(normalizeTopicType("initiative")).toBe("initiative");
    expect(normalizeTopicType("NOTE")).toBe("note");
  });

  it("falls back to other for unknown or empty values", () => {
    expect(normalizeTopicType("case")).toBe("other");
    expect(normalizeTopicType("")).toBe("other");
    expect(normalizeTopicType(null)).toBe("other");
  });

  it("returns semantic CSS var names for stroke/fill", () => {
    expect(topicTypeColorVarName("incident")).toBe("--danger");
    expect(topicTypeColorVarName("decision")).toBe("--accent");
    expect(topicTypeColorVarName("note")).toBe("--fg-muted");
  });

  it("keeps legacy topic types selectable for existing records", () => {
    expect(topicTypeSelectOptions("case").at(-1)).toEqual({
      value: "case",
      label: "Case (legacy)",
    });
    expect(topicTypeSelectOptions("decision").map((row) => row.value)).toEqual([
      "initiative",
      "objective",
      "decision",
      "incident",
      "risk",
      "request",
      "note",
      "other",
    ]);
  });
});
