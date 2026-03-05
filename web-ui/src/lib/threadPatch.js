const LIST_FIELDS = new Set(["tags", "next_actions", "key_artifacts"]);
const EDITABLE_FIELDS = [
  "title",
  "type",
  "status",
  "priority",
  "cadence",
  "next_check_in_at",
  "tags",
  "current_summary",
  "next_actions",
  "key_artifacts",
];

function normalizeList(value) {
  if (!Array.isArray(value)) {
    return [];
  }

  return value.map((item) => String(item).trim()).filter(Boolean);
}

function normalizeScalar(key, value) {
  if (key === "next_check_in_at") {
    return value ? String(value) : null;
  }

  return value ?? "";
}

export function buildThreadPatch(originalSnapshot = {}, draftSnapshot = {}) {
  const patch = {};

  for (const field of EDITABLE_FIELDS) {
    const originalValue = originalSnapshot[field];
    const draftValue = draftSnapshot[field];

    if (LIST_FIELDS.has(field)) {
      const normalizedOriginal = normalizeList(originalValue);
      const normalizedDraft = normalizeList(draftValue);

      if (
        JSON.stringify(normalizedOriginal) !== JSON.stringify(normalizedDraft)
      ) {
        patch[field] = normalizedDraft;
      }
      continue;
    }

    const normalizedOriginal = normalizeScalar(field, originalValue);
    const normalizedDraft = normalizeScalar(field, draftValue);

    if (normalizedOriginal !== normalizedDraft) {
      patch[field] = normalizedDraft;
    }
  }

  return patch;
}

export function parseListInput(rawValue) {
  return String(rawValue ?? "")
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function serializeListInput(items) {
  return normalizeList(items).join("\n");
}
