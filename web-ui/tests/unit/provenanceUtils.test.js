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
      sources: ["inferred", "event:event-1"],
    };
    const evidenceBacked = {
      sources: ["event:event-1", "receipt:artifact-1"],
    };

    expect(hasInferredProvenance(inferred)).toBe(true);
    expect(hasInferredProvenance(evidenceBacked)).toBe(false);
  });

  it("normalizes missing sources and returns deterministic presentation data", () => {
    expect(getProvenanceSources(undefined)).toEqual([]);
    expect(isUnknownProvenance(undefined)).toBe(true);
    expect(isUnknownProvenance({ sources: [] })).toBe(true);
    expect(isUnknownProvenance({ sources: ["event:event-1"] })).toBe(false);

    expect(getProvenancePresentation(undefined)).toEqual({
      unknown: true,
      inferred: false,
      title: "No provenance",
      toneClass: "border-slate-500/20 bg-slate-500/10 text-slate-400",
    });

    expect(getProvenancePresentation({ sources: ["inferred"] })).toEqual({
      unknown: false,
      inferred: true,
      title: "Inferred provenance",
      toneClass: "border-amber-500/20 bg-amber-500/10 text-amber-400",
    });

    expect(getProvenancePresentation({ sources: ["event:event-1"] })).toEqual({
      unknown: false,
      inferred: false,
      title: "Evidence-backed provenance",
      toneClass: "border-emerald-500/20 bg-emerald-500/10 text-emerald-400",
    });
  });
});
