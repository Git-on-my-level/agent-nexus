import { parseRef } from "./typedRefs.js";
import { validateCommitmentStatusRef } from "./eventRefRules.js";

const LIST_FIELDS = new Set(["definition_of_done", "links"]);
const EDITABLE_FIELDS = [
  "title",
  "owner",
  "due_at",
  "status",
  "definition_of_done",
  "links",
];

function normalizeList(value) {
  if (!Array.isArray(value)) {
    return [];
  }

  return value.map((item) => String(item).trim()).filter(Boolean);
}

function normalizeScalar(value) {
  return value ?? "";
}

export function parseCommitmentListInput(rawValue) {
  return String(rawValue ?? "")
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function serializeCommitmentListInput(items) {
  return normalizeList(items).join("\n");
}

export function buildCommitmentPatch(
  originalSnapshot = {},
  draftSnapshot = {},
) {
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

    const normalizedOriginal = normalizeScalar(originalValue);
    const normalizedDraft = normalizeScalar(draftValue);
    if (normalizedOriginal !== normalizedDraft) {
      patch[field] = normalizedDraft;
    }
  }

  return patch;
}

export function validateCommitmentStatusTransition(status, statusRefInput) {
  const nextStatus = String(status ?? "").trim();
  const refValue = String(statusRefInput ?? "").trim();

  if (nextStatus !== "done" && nextStatus !== "canceled") {
    return { valid: true, error: "" };
  }

  if (refValue) {
    const parsed = parseRef(refValue);
    if (!parsed.prefix || !parsed.value) {
      return {
        valid: false,
        error:
          "Status evidence ref must be a valid typed ref (<prefix>:<value>).",
      };
    }
  }

  return validateCommitmentStatusRef(nextStatus, refValue);
}
