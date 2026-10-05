import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";

import {
  countMarkdownTaskProgress,
  extractDocumentOutline,
  markdownExcerpt,
  markdownPlainText,
  renderMarkdown,
} from "../../src/lib/markdown.js";

const summaryFixtures = JSON.parse(
  readFileSync(
    new URL(
      "../../../contracts/fixtures/visual-reports/summaries.json",
      import.meta.url,
    ),
    "utf8",
  ),
);

describe("markdown", () => {
  for (const fixture of summaryFixtures) {
    it(`counts GFM tasks for ${fixture.name}`, () => {
      expect(countMarkdownTaskProgress(fixture.markdown)).toEqual(
        fixture.progress,
      );
    });
  }

  it("returns an empty string for empty or non-string input", () => {
    expect(renderMarkdown("")).toBe("");
    expect(renderMarkdown(null)).toBe("");
    expect(renderMarkdown(undefined)).toBe("");
    expect(renderMarkdown(42)).toBe("");
  });

  it("strips dangerous markup and attributes while preserving safe content", () => {
    const html = renderMarkdown(
      '<script>alert(1)</script><img src="/image.png" onerror="alert(1)" class="safe" data-test="drop-me">',
    );

    expect(html).not.toContain("<script");
    expect(html).toContain("<img");
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

  it("removes remote markdown images that would trigger browser tracking requests", () => {
    const html = renderMarkdown(
      "![x](https://attacker.example/pixel?id=abc123)",
    );

    expect(html).not.toContain("<img");
    expect(html).not.toContain("attacker.example");
    expect(html).not.toContain("pixel?id=");
  });

  it("preserves local and non-network markdown images", () => {
    expect(renderMarkdown("![local](/attachments/image.png)")).toContain(
      '<img src="/attachments/image.png"',
    );
    expect(renderMarkdown("![relative](./image.png)")).toContain(
      '<img src="./image.png"',
    );
    expect(renderMarkdown("![inline](data:image/png;base64,AAAA)")).toContain(
      '<img src="data:image/png;base64,AAAA"',
    );
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
    expect(renderMarkdown("![alt](/img.png)")).toContain("<img");
    expect(renderMarkdown("> quote")).toContain("<blockquote");
    expect(renderMarkdown("**bold**")).toContain("<strong");
    expect(renderMarkdown("*italic*")).toContain("<em");
    expect(renderMarkdown("~~strikethrough~~")).toContain("<del");
  });

  it("keeps fenced list examples out of task controls", () => {
    const html = renderMarkdown(
      "-   Container\n    ```markdown\n    - [x] Example\n    ```\n- [ ] Real task",
    );

    expect(html.match(/type="checkbox"/g) ?? []).toHaveLength(1);
    expect(html).toContain("- [x] Example");
  });

  it("normalizes outbound links with safe rel and target attributes", () => {
    const result = renderMarkdown("[link](https://example.com)");

    expect(result).toContain('rel="noopener noreferrer"');
    expect(result).toContain('target="_blank"');
  });

  it("adds slugged anchor ids to rendered headings", () => {
    const html = renderMarkdown("# Getting Started\n\n## API & Usage");
    expect(html).toContain('<h1 id="getting-started"');
    expect(html).toContain('<h2 id="api-usage"');
  });

  it("de-duplicates repeated heading slugs deterministically", () => {
    const html = renderMarkdown("# Notes\n\n## Notes\n\n### Notes");
    expect(html).toContain('id="notes"');
    expect(html).toContain('id="notes-1"');
    expect(html).toContain('id="notes-2"');
  });
});

describe("extractDocumentOutline", () => {
  it("returns an empty list for empty or non-string input", () => {
    expect(extractDocumentOutline("")).toEqual([]);
    expect(extractDocumentOutline(null)).toEqual([]);
    expect(extractDocumentOutline(undefined)).toEqual([]);
  });

  it("extracts H1-H3 headings with level, plain text, and slug id", () => {
    const outline = extractDocumentOutline(
      "# Title\n\nintro\n\n## Section **one**\n\n### Detail `code`\n\n#### Too deep",
    );
    expect(outline).toEqual([
      { level: 1, text: "Title", id: "title" },
      { level: 2, text: "Section one", id: "section-one" },
      { level: 3, text: "Detail code", id: "detail-code" },
    ]);
  });

  it("keeps slug ids aligned with the rendered anchors when duplicated", () => {
    const source = "# Notes\n\n## Notes";
    const outline = extractDocumentOutline(source);
    const html = renderMarkdown(source);
    for (const heading of outline) {
      expect(html).toContain(`id="${heading.id}"`);
    }
    expect(outline.map((h) => h.id)).toEqual(["notes", "notes-1"]);
  });

  it("advances dedupe counters for skipped deep headings so ids still match render", () => {
    // An h4 "Notes" sits between two surfaced headings; the renderer slugs it
    // (notes-1), so the second visible "Notes" must become notes-2.
    const source = "# Notes\n\n#### Notes\n\n## Notes";
    const outline = extractDocumentOutline(source);
    const html = renderMarkdown(source);
    expect(outline.map((h) => h.id)).toEqual(["notes", "notes-2"]);
    for (const heading of outline) {
      expect(html).toContain(`id="${heading.id}"`);
    }
  });
});

describe("renderMarkdown GFM and HTML handling", () => {
  it("renders task lists as checkboxes", () => {
    const html = renderMarkdown("- [ ] open\n- [x] done\n");
    expect(html).toContain('type="checkbox"');
    expect(html).toContain("checked");
    expect(html).toContain("open");
    expect(html).toContain("done");
  });

  it("renders tables", () => {
    const html = renderMarkdown("| a | b |\n|---|---|\n| 1 | 2 |\n");
    expect(html).toContain("<table>");
    expect(html).toContain("<th>a</th>");
    expect(html).toContain("<td>1</td>");
  });

  it("renders fenced code blocks without chipping refs inside them", () => {
    const html = renderMarkdown("```\ncard:example\n```\n");
    expect(html).toContain("<pre>");
    expect(html).toContain("card:example");
    expect(html).not.toContain("data-md-ref");
  });

  it("autolinks a bare URL", () => {
    const html = renderMarkdown("see https://example.test/x now");
    expect(html).toContain('href="https://example.test/x"');
  });

  it("renders a details/summary disclosure", () => {
    const html = renderMarkdown(
      "<details><summary>Why</summary>\n\nBecause.\n\n</details>\n",
    );
    expect(html).toContain("<details>");
    expect(html).toContain("<summary>Why</summary>");
    expect(html).toContain("Because.");
  });

  it("hides HTML comments such as the fleet-sync evidence marker", () => {
    const html = renderMarkdown(
      "<!-- fleet-sync:evidence:v1 -->\n\nVisible body.\n",
    );
    expect(html).not.toContain("fleet-sync");
    expect(html).not.toContain("<!--");
    expect(html).toContain("Visible body.");
  });

  it("hides an inline HTML comment without eating the prose around it", () => {
    const html = renderMarkdown("before <!-- note --> after");
    expect(html).not.toContain("note");
    expect(html).toContain("before");
    expect(html).toContain("after");
  });
});

describe("renderMarkdown ref chips", () => {
  it("emits a chip placeholder for an ANX ref in prose", () => {
    const html = renderMarkdown("Blocked by card:release-b until Friday.");
    expect(html).toContain('data-md-ref="card:release-b"');
    expect(html).toContain("Blocked by ");
    expect(html).toContain(" until Friday.");
  });

  it("emits a chip placeholder for an autolinked GitHub pull request", () => {
    const html = renderMarkdown("fixed in https://github.com/o/r/pull/12");
    expect(html).toContain('data-md-ref="https://github.com/o/r/pull/12"');
    // The chip is the destination, so no anchor is emitted for it.
    expect(html).not.toContain('<a href="https://github.com/o/r/pull/12"');
    // The placeholder text is the readable label, not the raw URL.
    expect(html).toContain("o/r#12");
  });

  it("leaves a labelled link alone rather than nesting a chip inside it", () => {
    const html = renderMarkdown("[the fix](https://github.com/o/r/pull/12)");
    expect(html).toContain('href="https://github.com/o/r/pull/12"');
    expect(html).not.toContain("data-md-ref");
  });

  it("does not chip a ref written inside a link label", () => {
    const html = renderMarkdown("[card:release-b](https://example.test)");
    expect(html).not.toContain("data-md-ref");
    expect(html).toContain("card:release-b");
  });

  it("does not chip a ref inside a code span", () => {
    const html = renderMarkdown("write `card:release-b` to point at it");
    expect(html).toContain("<code>card:release-b</code>");
    expect(html).not.toContain("data-md-ref");
  });

  it("can be turned off", () => {
    const html = renderMarkdown("Blocked by card:release-b.", {
      refChips: false,
    });
    expect(html).not.toContain("data-md-ref");
    expect(html).toContain("card:release-b");
  });

  it("chips refs in inline mode too", () => {
    const html = renderMarkdown("card:release-b moves", { inline: true });
    expect(html).toContain('data-md-ref="card:release-b"');
    expect(html).not.toContain("<p>");
  });

  it("keeps a ref value out of href position even if it looks executable", () => {
    const html = renderMarkdown("[x](javascript:alert(1))");
    expect(html).not.toContain("javascript:");
  });
});

describe("markdownExcerpt", () => {
  it("returns an empty string for empty or non-string input", () => {
    expect(markdownExcerpt("")).toBe("");
    expect(markdownExcerpt(null)).toBe("");
    expect(markdownExcerpt(42)).toBe("");
  });

  it("strips inline markdown so a tile never shows **Goal:**", () => {
    expect(markdownExcerpt("**Goal:** ship the renderer\n")).toBe(
      "Goal: ship the renderer",
    );
  });

  it("skips a leading fenced code block and uses the first prose line", () => {
    expect(markdownExcerpt("```\nnot prose\n```\n\nReal line.\n")).toBe(
      "Real line.",
    );
  });

  it("skips a leading HTML comment marker", () => {
    expect(
      markdownExcerpt("<!-- fleet-sync:evidence:v1 -->\n\nReal line.\n"),
    ).toBe("Real line.");
  });

  it("flattens a heading, a link and a code span", () => {
    expect(markdownExcerpt("# Release [B](https://e.test) uses `anx`\n")).toBe(
      "Release B uses anx",
    );
  });

  it("uses the first list item when the body opens with a list", () => {
    expect(markdownExcerpt("- [x] first step\n- second\n")).toBe("first step");
  });

  it("collapses newlines inside one paragraph", () => {
    expect(markdownExcerpt("one\ntwo\nthree\n")).toBe("one two three");
  });

  it("truncates on a word boundary with an ellipsis", () => {
    const excerpt = markdownExcerpt(
      "alpha bravo charlie delta echo foxtrot golf",
      { limit: 20 },
    );
    expect(excerpt.endsWith("…")).toBe(true);
    expect(excerpt.length).toBeLessThanOrEqual(21);
    expect(excerpt).not.toContain("  ");
  });
});

describe("markdownPlainText", () => {
  it("flattens a whole body into one line of prose", () => {
    expect(markdownPlainText("# Title\n\n**Goal:** a\n\n- one\n- two\n")).toBe(
      "Title Goal: a one two",
    );
  });

  it("returns an empty string for empty or non-string input", () => {
    expect(markdownPlainText("")).toBe("");
    expect(markdownPlainText(null)).toBe("");
  });
});
