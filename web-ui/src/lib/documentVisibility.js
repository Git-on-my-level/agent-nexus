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
