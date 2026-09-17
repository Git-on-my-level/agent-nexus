import { describe, expect, it } from "vitest";

import { decisionIdsFromTurn } from "../../src/lib/pm/turnDecisions.js";

describe("decisionIdsFromTurn", () => {
  it("collects decision refs from evidence", () => {
    expect(
      decisionIdsFromTurn({
        evidence_refs: ["card:restock", "decision:abc-1"],
      }),
    ).toEqual(["abc-1"]);
  });

  it("collects decision mentions from the reply text", () => {
    expect(
      decisionIdsFromTurn({
        response: "Proposed decision:123 and decision:456 (see decision:123).",
      }),
    ).toEqual(["123", "456"]);
  });

  it("merges evidence and text without duplicates, evidence first", () => {
    expect(
      decisionIdsFromTurn({
        evidence_refs: ["decision:ev-1"],
        response: "see decision:txt-1 and decision:ev-1",
      }),
    ).toEqual(["ev-1", "txt-1"]);
  });

  it("extracts the typed tail of hyphenated mentions, like the runner regex", () => {
    expect(
      decisionIdsFromTurn({
        evidence_refs: ["card:restock", "document:kb"],
        response: "not-a-decision:ref decision:",
      }),
    ).toEqual(["ref"]);
  });

  it("tolerates missing turns", () => {
    expect(decisionIdsFromTurn(null)).toEqual([]);
    expect(decisionIdsFromTurn({})).toEqual([]);
  });
});
