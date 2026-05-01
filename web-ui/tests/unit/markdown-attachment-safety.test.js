import { describe, expect, it } from "vitest";
import { renderMarkdown } from "../../src/lib/markdown.js";

describe("renderMarkdown (attachment-facing safety)", () => {
  it("strips script tags from untrusted markdown", () => {
    const html = renderMarkdown("Hello `<script>alert(1)</script>`");
    expect(html.toLowerCase()).not.toContain("<script");
  });

  it("does not preserve event handler attributes from inline HTML", () => {
    const html = renderMarkdown('<span onclick="alert(1)">x</span>');
    expect(html.toLowerCase()).not.toContain("onclick");
  });
});
