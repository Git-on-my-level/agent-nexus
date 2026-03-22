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

    expect(html).not.toContain("<script");
    expect(html).not.toContain("onerror=");
    expect(html).not.toContain("data-test=");
  });

  it("adds safe anchor defaults and strips javascript urls", () => {
    expect(renderMarkdown("[safe](https://example.com/path)")).toContain(
      '<a href="https://example.com/path"',
    );
    expect(renderMarkdown("[safe](https://example.com/path)")).toContain(
      'rel="noopener noreferrer"',
    );
    expect(renderMarkdown("[safe](https://example.com/path)")).toContain(
      'target="_blank"',
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

  it("strips inline event handlers from all elements", () => {
    const cases = [
      {
        input: '<img src="x" onerror="alert(1)">',
        shouldNotContain: "onerror",
      },
      {
        input: '<div onclick="alert(1)">test</div>',
        shouldNotContain: "onclick",
      },
      {
        input: '<a href="x" onmouseover="alert(1)">link</a>',
        shouldNotContain: "onmouseover",
      },
      {
        input: '<input onfocus="alert(1)">',
        shouldNotContain: "onfocus",
      },
      {
        input: '<body onload="alert(1)">',
        shouldNotContain: "onload",
      },
    ];

    cases.forEach(({ input, shouldNotContain }) => {
      const result = renderMarkdown(input);
      expect(result).not.toContain(shouldNotContain);
    });
  });

  it("blocks javascript: and data: URI schemes", () => {
    const cases = [
      {
        input: "[click](javascript:alert(1))",
        shouldNotContain: "javascript:",
      },
      {
        input: "[click](JAVASCRIPT:alert(1))",
        shouldNotContain: "JAVASCRIPT:",
      },
      {
        input: "[click](  javascript:alert(1))",
        shouldNotContain: "javascript:",
      },
      {
        input: "[click](data:text/html,<script>alert(1)</script>)",
        shouldNotContain: "data:",
      },
      {
        input: '<a href="javascript:void(0)">link</a>',
        shouldNotContain: "javascript:",
      },
    ];

    cases.forEach(({ input, shouldNotContain }) => {
      const result = renderMarkdown(input);
      expect(result.toLowerCase()).not.toContain(
        shouldNotContain.toLowerCase(),
      );
    });
  });

  it("strips script, iframe, object, and embed tags", () => {
    const cases = [
      { input: "<script>alert(1)</script>", shouldNotContain: "<script" },
      { input: "<iframe src='x'></iframe>", shouldNotContain: "<iframe" },
      { input: "<object data='x'></object>", shouldNotContain: "<object" },
      { input: "<embed src='x'>", shouldNotContain: "<embed" },
    ];

    cases.forEach(({ input, shouldNotContain }) => {
      const result = renderMarkdown(input);
      expect(result).not.toContain(shouldNotContain);
    });
  });

  it("handles malformed HTML intended to bypass regex stripping", () => {
    const cases = [
      {
        input: '<img src="x" onerror=alert(1)>',
        shouldNotContain: "onerror",
      },
      {
        input: "<SCRIPT>alert(1)</SCRIPT>",
        shouldNotContain: "<script",
      },
      {
        input: "<ScRiPt>alert(1)</ScRiPt>",
        shouldNotContain: "<script",
      },
      {
        input: "<div onmouseover=alert(1)>test",
        shouldNotContain: "onmouseover",
      },
    ];

    cases.forEach(({ input, shouldNotContain }) => {
      const result = renderMarkdown(input);
      expect(result.toLowerCase()).not.toContain(
        shouldNotContain.toLowerCase(),
      );
    });
  });

  it("preserves safe markdown features", () => {
    expect(renderMarkdown("# Heading 1")).toContain("<h1");
    expect(renderMarkdown("## Heading 2")).toContain("<h2");
    expect(renderMarkdown("- item 1\n- item 2")).toContain("<ul");
    expect(renderMarkdown("1. item 1\n2. item 2")).toContain("<ol");
    expect(renderMarkdown("- [ ] task")).toContain('type="checkbox"');
    expect(renderMarkdown("```js\ncode\n```")).toContain("<pre");
    expect(renderMarkdown("| a | b |\n|---|---|\n| 1 | 2 |")).toContain(
      "<table",
    );
    expect(renderMarkdown("[link](https://example.com)")).toContain("<a");
    expect(renderMarkdown("![alt](https://example.com/img.png)")).toContain(
      "<img",
    );
    expect(renderMarkdown("> quote")).toContain("<blockquote");
    expect(renderMarkdown("**bold**")).toContain("<strong");
    expect(renderMarkdown("*italic*")).toContain("<em");
    expect(renderMarkdown("~~strikethrough~~")).toContain("<del");
  });

  it("normalizes outbound links with safe rel and target attributes", () => {
    const result = renderMarkdown("[link](https://example.com)");

    expect(result).toContain('rel="noopener noreferrer"');
    expect(result).toContain('target="_blank"');
  });
});
