function normalizedLabels(doc) {
  return Array.isArray(doc?.labels) ? doc.labels : [];
}

export function isLegacyAgentRegistrationDocument(doc) {
  const documentId = String(doc?.id ?? "").trim();
  if (documentId.startsWith("agentreg.")) return true;
  return normalizedLabels(doc).includes("agent-registration");
}

export function filterTopLevelDocuments(documents) {
  if (!Array.isArray(documents)) return [];
  return documents.filter((doc) => !isLegacyAgentRegistrationDocument(doc));
}

const DOC_LIFECYCLE_LABELS = {
  active: "Active",
  archived: "Archived",
  trashed: "Trashed",
  draft: "Draft",
};

/** API uses `state`; `status` is a legacy/compat fallback. */
export function documentResourceState(doc) {
  if (!doc || typeof doc !== "object") return "";
  return String(doc.state ?? doc.status ?? "").trim();
}

export function documentLifecycleLabel(state) {
  if (!state) return "";
  return DOC_LIFECYCLE_LABELS[state] ?? state;
}

export function documentLifecyclePillClass(state) {
  if (state === "active") return "text-ok-text bg-ok-soft";
  if (state === "archived") return "text-warn-text bg-warn-soft";
  if (state === "trashed") return "text-danger-text bg-danger-soft";
  if (state === "draft") return "text-warn-text bg-warn-soft";
  return "text-[var(--fg-muted)] bg-[var(--line)]";
}
