import { describe, expect, it } from "vitest";

import {
  diffCardRevisionAgainstParent,
  humanizeRevisionFieldKey,
} from "../../src/lib/textDiff.js";

describe("textDiff card revisions", () => {
  it("humanizes field keys", () => {
    expect(humanizeRevisionFieldKey("definition_of_done")).toBe(
      "Definition Of Done",
    );
  });

  it("reports title change between parent and child revision", () => {
    const parent = {
      revision_number: 1,
      title: "A",
      summary: "same",
      definition_of_done: [],
    };
    const rev = {
      revision_number: 2,
      title: "B",
      summary: "same",
      definition_of_done: [],
    };
    const d = diffCardRevisionAgainstParent(parent, rev);
    const title = d.find((x) => x.field === "title");
    expect(title).toBeTruthy();
    expect(title?.lines.some((l) => l.kind === "remove")).toBe(true);
    expect(title?.lines.some((l) => l.kind === "add")).toBe(true);
  });
});
