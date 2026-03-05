export function getProvenanceSources(provenance) {
  if (!Array.isArray(provenance?.sources)) {
    return [];
  }

  return provenance.sources.map((source) => String(source));
}

export function isUnknownProvenance(provenance) {
  return getProvenanceSources(provenance).length === 0;
}

export function hasInferredProvenance(provenance) {
  const sources = getProvenanceSources(provenance);
  return sources.some((source) => source.toLowerCase().includes("inferred"));
}

export function getProvenancePresentation(provenance) {
  if (isUnknownProvenance(provenance)) {
    return {
      unknown: true,
      inferred: false,
      title: "No provenance",
      toneClass: "border-slate-300 bg-slate-50 text-slate-700",
    };
  }

  const inferred = hasInferredProvenance(provenance);

  return {
    unknown: false,
    inferred,
    title: inferred ? "Inferred provenance" : "Evidence-backed provenance",
    toneClass: inferred
      ? "border-amber-300 bg-amber-50 text-amber-900"
      : "border-emerald-300 bg-emerald-50 text-emerald-900",
  };
}
