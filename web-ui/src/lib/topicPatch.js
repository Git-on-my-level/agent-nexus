const EDITABLE_FIELDS = ["title", "summary"];

function normalizeScalar(value) {
  return value ?? "";
}

export function buildTopicPatch(original = {}, draft = {}) {
  const patch = {};

  for (const field of EDITABLE_FIELDS) {
    const originalValue = original[field];
    const draftValue = draft[field];

    const normalizedOriginal = normalizeScalar(originalValue);
    const normalizedDraft = normalizeScalar(draftValue);

    if (normalizedOriginal !== normalizedDraft) {
      patch[field] = normalizedDraft;
    }
  }

  return patch;
}

export { parseListInput, serializeListInput } from "./typedRefs.js";
