import { describe, expect, it } from "vitest";

import {
  getProvenancePresentation,
  getProvenanceSources,
  hasInferredProvenance,
} from "../../src/lib/provenanceUtils.js";

describe("provenance utils", () => {
  it("treats inferred provenance distinctly from evidence-backed provenance", () => {
    const inferred = {
      sources: ["inferred", "actor_statement:event-1"],
    };
    const evidenceBacked = {
      sources: ["actor_statement:event-1", "receipt:artifact-1"],
    };

    expect(hasInferredProvenance(inferred)).toBe(true);
    expect(hasInferredProvenance(evidenceBacked)).toBe(false);
  });

  it("normalizes missing sources and returns deterministic presentation data", () => {
    expect(getProvenanceSources(undefined)).toEqual([]);

    expect(getProvenancePresentation({ sources: ["inferred"] })).toEqual({
      inferred: true,
      title: "Inferred provenance",
      toneClass: "border-amber-300 bg-amber-50 text-amber-900",
    });

    expect(
      getProvenancePresentation({ sources: ["actor_statement:event-1"] }),
    ).toEqual({
      inferred: false,
      title: "Evidence-backed provenance",
      toneClass: "border-emerald-300 bg-emerald-50 text-emerald-900",
    });
  });
});
