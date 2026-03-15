import { describe, expect, it } from "vitest";

import { renderMarkdown } from "../../src/lib/markdown.js";

describe("markdown", () => {
  it("returns an empty string for empty or non-string input", () => {
    expect(renderMarkdown("")).toBe("");
    expect(renderMarkdown(null)).toBe("");
    expect(renderMarkdown(undefined)).toBe("");
    expect(renderMarkdown(42)).toBe("");
  });

  it("strips dangerous markup and attributes while preserving safe content", () => {
    const html = renderMarkdown(
      '<script>alert(1)</script><img src="https://example.com/image.png" onerror="alert(1)" class="safe" data-test="drop-me">',
    );

    expect(html).toContain(
      'alert(1)<img src="https://example.com/image.png" class="safe" />',
    );
    expect(html).not.toContain("<script");
    expect(html).not.toContain("onerror=");
    expect(html).not.toContain("data-test=");
  });

  it("adds safe anchor defaults and strips javascript urls", () => {
    expect(renderMarkdown("[safe](https://example.com/path)")).toContain(
      '<a href="https://example.com/path" rel="noopener noreferrer" target="_blank">safe</a>',
    );

    const unsafeLink = renderMarkdown("[unsafe](javascript:alert(1))");
    expect(unsafeLink).toContain(
      '<a rel="noopener noreferrer" target="_blank">unsafe</a>',
    );
    expect(unsafeLink).not.toContain('href="javascript:alert(1)"');
  });

  it("supports inline rendering without paragraph wrappers", () => {
    expect(renderMarkdown("**inline**", { inline: true })).toBe(
      "<strong>inline</strong>",
    );
  });
});
