export const KIND_LABELS = {
  doc: "Document",
  card: "Card",
  agent_wake: "Agent wake",
  attachment: "Attachment",
};

const KIND_DESCRIPTIONS = {
  doc: "Readable document artifact",
  card: "Immutable card revision content",
  agent_wake: "Agent wake payload",
  attachment: "External document or text blob",
};

const KIND_COLORS = {
  doc: "text-fuchsia-400 bg-fuchsia-500/10",
  card: "text-sky-400 bg-sky-500/10",
  agent_wake: "text-teal-400 bg-teal-500/10",
  attachment: "text-amber-300 bg-amber-500/10",
};

const FALLBACK_COLOR = "text-[var(--fg-muted)] bg-[var(--line)]";

export function kindLabel(kind) {
  return KIND_LABELS[String(kind ?? "").trim()] ?? String(kind ?? "Artifact");
}

export function kindDescription(kind) {
  return KIND_DESCRIPTIONS[kind] ?? "Artifact payload";
}

export function kindColor(kind) {
  return KIND_COLORS[kind] ?? FALLBACK_COLOR;
}
