export const KIND_LABELS = {
  receipt: "Receipt",
  review: "Review",
  doc: "Document",
  evidence: "Evidence",
  log: "Log",
};

const KIND_DESCRIPTIONS = {
  receipt: "Work completion evidence and verification",
  review: "Human decision on receipt quality",
  doc: "Readable document artifact",
  evidence: "Supporting evidence and logs",
  log: "Operational activity record",
};

const KIND_COLORS = {
  receipt: "text-emerald-400 bg-emerald-500/10",
  review: "text-amber-400 bg-amber-500/10",
  doc: "text-fuchsia-400 bg-fuchsia-500/10",
  evidence: "text-[var(--ui-text-muted)] bg-[var(--ui-border)]",
  log: "text-teal-400 bg-teal-500/10",
};

const FALLBACK_COLOR = "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";

export function kindLabel(kind) {
  return KIND_LABELS[String(kind ?? "").trim()] ?? String(kind ?? "Artifact");
}

export function kindDescription(kind) {
  return KIND_DESCRIPTIONS[kind] ?? "Artifact payload";
}

export function kindColor(kind) {
  return KIND_COLORS[kind] ?? FALLBACK_COLOR;
}
