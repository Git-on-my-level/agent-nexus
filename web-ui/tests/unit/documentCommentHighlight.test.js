// @vitest-environment jsdom

import { describe, expect, it } from "vitest";

import {
  applyDocumentCommentHighlights,
  clearDocumentCommentMarks,
  highlightDocumentCommentRange,
} from "../../src/lib/documentCommentHighlight.js";

function makeRoot(html) {
  const root = document.createElement("div");
  root.innerHTML = html;
  return root;
}

describe("documentCommentHighlight", () => {
  it("wraps the first occurrence of a quote in a <mark>", () => {
    const root = makeRoot(
      "<p>This is my doc</p><p>Second line</p><p>Whatever</p>",
    );
    const ok = highlightDocumentCommentRange(root, "Second line");
    expect(ok).toBe(true);
    const marks = root.querySelectorAll("mark[data-doc-comment-mark='1']");
    expect(marks.length).toBe(1);
    expect(marks[0].textContent).toBe("Second line");
  });

  it("clearDocumentCommentMarks unwraps inserted marks", () => {
    const root = makeRoot("<p>This is my doc</p><p>Second line</p>");
    expect(highlightDocumentCommentRange(root, "Second line")).toBe(true);
    expect(root.querySelectorAll("mark").length).toBe(1);
    clearDocumentCommentMarks(root);
    expect(root.querySelectorAll("mark").length).toBe(0);
    expect(root.textContent).toBe("This is my docSecond line");
  });

  it("returns false for an empty quote and leaves the DOM untouched", () => {
    const root = makeRoot("<p>Hello</p>");
    const before = root.innerHTML;
    expect(highlightDocumentCommentRange(root, "")).toBe(false);
    expect(highlightDocumentCommentRange(root, "   ")).toBe(false);
    expect(root.innerHTML).toBe(before);
  });

  it("returns false when the quote is not present in the body text", () => {
    const root = makeRoot("<p>Hello</p>");
    expect(highlightDocumentCommentRange(root, "Goodbye")).toBe(false);
    expect(root.querySelectorAll("mark").length).toBe(0);
  });

  it("skips text inside <code> blocks so syntax styling stays intact", () => {
    const root = makeRoot(
      "<p>Read <code>foo</code> carefully</p><p>foo bar</p>",
    );
    const ok = highlightDocumentCommentRange(root, "foo");
    expect(ok).toBe(true);
    const marks = root.querySelectorAll("mark[data-doc-comment-mark='1']");
    expect(marks.length).toBe(1);
    // The mark must be the one inside the second paragraph, not inside <code>.
    expect(marks[0].closest("code")).toBeNull();
    expect(marks[0].textContent).toBe("foo");
  });

  it("replaces a previous highlight when called again with a new quote", () => {
    const root = makeRoot("<p>Alpha beta gamma</p>");
    expect(highlightDocumentCommentRange(root, "Alpha")).toBe(true);
    expect(root.querySelector("mark").textContent).toBe("Alpha");
    expect(highlightDocumentCommentRange(root, "gamma")).toBe(true);
    const marks = root.querySelectorAll("mark");
    expect(marks.length).toBe(1);
    expect(marks[0].textContent).toBe("gamma");
  });

  it("wraps a quote that crosses inline element boundaries via fallback path", () => {
    const root = makeRoot("<p>Hello <em>brave</em> new world</p>");
    const ok = highlightDocumentCommentRange(root, "brave new");
    expect(ok).toBe(true);
    const marks = root.querySelectorAll("mark[data-doc-comment-mark='1']");
    expect(marks.length).toBe(1);
    expect(marks[0].textContent.replace(/\s+/g, " ").trim()).toBe("brave new");
  });

  it("applyDocumentCommentHighlights renders two posted marks with data-event-id", () => {
    const root = makeRoot(
      "<p>First unique line here</p><p>Second unique line there</p>",
    );
    applyDocumentCommentHighlights(root, {
      posted: [
        { quote: "First unique line here", eventId: "evt-a" },
        { quote: "Second unique line there", eventId: "evt-b" },
      ],
      pendingQuote: "",
    });
    const marks = root.querySelectorAll("mark[data-doc-comment-mark='1']");
    expect(marks.length).toBe(2);
    expect(marks[0].getAttribute("data-event-id")).toBe("evt-a");
    expect(marks[1].getAttribute("data-event-id")).toBe("evt-b");
  });

  it("applyDocumentCommentHighlights prefers pending over a matching posted quote", () => {
    const root = makeRoot("<p>Only one line of text</p>");
    applyDocumentCommentHighlights(root, {
      posted: [{ quote: "Only one line of text", eventId: "evt-1" }],
      pendingQuote: "Only one line of text",
    });
    const marks = root.querySelectorAll("mark[data-doc-comment-mark='1']");
    expect(marks.length).toBe(1);
    expect(marks[0].className).toContain("is-pending");
    expect(marks[0].hasAttribute("data-event-id")).toBe(false);
  });
});
