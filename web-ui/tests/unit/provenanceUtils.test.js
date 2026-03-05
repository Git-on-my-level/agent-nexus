import { describe, expect, it } from "vitest";

import {
  getProvenancePresentation,
  getProvenanceSources,
  hasInferredProvenance,
  isUnknownProvenance,
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
    expect(isUnknownProvenance(undefined)).toBe(true);
    expect(isUnknownProvenance({ sources: [] })).toBe(true);
    expect(isUnknownProvenance({ sources: ["actor_statement:event-1"] })).toBe(
      false,
    );

    expect(getProvenancePresentation(undefined)).toEqual({
      unknown: true,
      inferred: false,
      title: "No provenance",
      toneClass: "border-slate-300 bg-slate-50 text-slate-700",
    });

    expect(getProvenancePresentation({ sources: ["inferred"] })).toEqual({
      unknown: false,
      inferred: true,
      title: "Inferred provenance",
      toneClass: "border-amber-300 bg-amber-50 text-amber-900",
    });

    expect(
      getProvenancePresentation({ sources: ["actor_statement:event-1"] }),
    ).toEqual({
      unknown: false,
      inferred: false,
      title: "Evidence-backed provenance",
      toneClass: "border-emerald-300 bg-emerald-50 text-emerald-900",
    });
  });
});
