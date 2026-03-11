import { describe, expect, it } from "vitest";

import {
  isKnownRefPrefix,
  parseRef,
  renderRef,
} from "../../src/lib/typedRefs.js";

describe("typed refs", () => {
  it("parses known prefixes", () => {
    expect(parseRef("artifact:art-123")).toEqual({
      prefix: "artifact",
      value: "art-123",
    });
    expect(parseRef("snapshot:snap-1")).toEqual({
      prefix: "snapshot",
      value: "snap-1",
    });
    expect(parseRef("event:evt-7")).toEqual({
      prefix: "event",
      value: "evt-7",
    });
    expect(parseRef("thread:thread-9")).toEqual({
      prefix: "thread",
      value: "thread-9",
    });
    expect(parseRef("url:https://example.com/path?a=b")).toEqual({
      prefix: "url",
      value: "https://example.com/path?a=b",
    });
    expect(parseRef("inbox:item-4")).toEqual({
      prefix: "inbox",
      value: "item-4",
    });
    expect(parseRef("document:doc-1")).toEqual({
      prefix: "document",
      value: "doc-1",
    });
    expect(parseRef("document_revision:rev-1")).toEqual({
      prefix: "document_revision",
      value: "rev-1",
    });
  });

  it("preserves unknown prefixes and renders back to raw string", () => {
    const parsed = parseRef("custom:opaque-value");
    expect(parsed).toEqual({ prefix: "custom", value: "opaque-value" });
    expect(isKnownRefPrefix(parsed.prefix)).toBe(false);
    expect(renderRef(parsed)).toBe("custom:opaque-value");
  });

  it("treats document prefixes as known refs", () => {
    expect(isKnownRefPrefix("document")).toBe(true);
    expect(isKnownRefPrefix("document_revision")).toBe(true);
  });
});
