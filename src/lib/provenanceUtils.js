export function getProvenanceSources(provenance) {
  if (!Array.isArray(provenance?.sources)) {
    return [];
  }

  return provenance.sources.map((source) => String(source));
}

export function hasInferredProvenance(provenance) {
  const sources = getProvenanceSources(provenance);
  return sources.some((source) => source.toLowerCase().includes("inferred"));
}

export function getProvenancePresentation(provenance) {
  const inferred = hasInferredProvenance(provenance);

  return {
    inferred,
    title: inferred ? "Inferred provenance" : "Evidence-backed provenance",
    toneClass: inferred
      ? "border-amber-300 bg-amber-50 text-amber-900"
      : "border-emerald-300 bg-emerald-50 text-emerald-900",
  };
}
