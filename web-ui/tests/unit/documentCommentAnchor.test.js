import { describe, expect, it } from "vitest";

import { buildDocumentCommentFields } from "../../src/lib/documentCommentAnchor.js";

describe("buildDocumentCommentFields", () => {
  it("returns quote_only with empty offsets when the quote is not in the source", () => {
    const r = buildDocumentCommentFields({
      source: "Hello world",
      selectedText: "nope",
      documentId: "d1",
      revisionId: "r1",
      contentHash: "h1",
      isHeadRevision: true,
    });
    expect(r.anchor_status).toBe("quote_only");
    expect(r.start_offset).toBeNull();
    expect(r.end_offset).toBeNull();
    expect(r.selected_text).toBe("nope");
  });

  it("maps a unique exact substring to offsets and current status on head", () => {
    const src = "alpha\n## Section\nline";
    const r = buildDocumentCommentFields({
      source: src,
      selectedText: "## Section",
      documentId: "d1",
      revisionId: "r1",
      isHeadRevision: true,
    });
    expect(r.anchor_status).toBe("current");
    expect(r.start_offset).toBe(6);
    expect(r.end_offset).toBe(6 + "## Section".length);
    expect(r.selected_text).toBe("## Section");
    expect(r.context_before.length).toBeGreaterThan(0);
  });

  it("uses historical when revision is not head but mapping is unique", () => {
    const r = buildDocumentCommentFields({
      source: "only once",
      selectedText: "only",
      documentId: "d1",
      revisionId: "r-old",
      isHeadRevision: false,
    });
    expect(r.anchor_status).toBe("historical");
    expect(r.start_offset).toBe(0);
  });

  it("sets quote_only when the selection matches multiple times", () => {
    const r = buildDocumentCommentFields({
      source: "foo foo foo",
      selectedText: "foo",
      documentId: "d1",
      revisionId: "r1",
    });
    expect(r.anchor_status).toBe("quote_only");
    expect(r.start_offset).toBeNull();
  });
});
