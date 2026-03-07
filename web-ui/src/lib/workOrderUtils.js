import { parseRef } from "./typedRefs.js";

export function parseWorkOrderListInput(rawValue) {
  return String(rawValue ?? "")
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function serializeWorkOrderListInput(items) {
  if (!Array.isArray(items)) {
    return "";
  }

  return items
    .map((item) => String(item).trim())
    .filter(Boolean)
    .join("\n");
}

export function validateTypedRefs(refs = []) {
  const invalidRefs = [];

  refs.forEach((refValue) => {
    const parsed = parseRef(refValue);
    if (!parsed.prefix || !parsed.value) {
      invalidRefs.push(refValue);
    }
  });

  return {
    valid: invalidRefs.length === 0,
    invalidRefs,
  };
}

export function ensureThreadRef(refs = [], threadId) {
  const normalized = refs.map((item) => String(item).trim()).filter(Boolean);
  const threadRef = `thread:${threadId}`;

  if (!normalized.includes(threadRef)) {
    normalized.unshift(threadRef);
  }

  return normalized;
}

export function validateWorkOrderDraft(draft, options = {}) {
  const threadId = String(options.threadId ?? "").trim();
  const errors = [];
  const fieldErrors = {};

  function addError(field, message) {
    errors.push(message);
    if (!fieldErrors[field]) fieldErrors[field] = [];
    fieldErrors[field].push(message);
  }

  const objective = String(draft?.objective ?? "").trim();
  const constraints = parseWorkOrderListInput(draft?.constraintsInput);
  const contextRefs = ensureThreadRef(
    parseWorkOrderListInput(draft?.contextRefsInput),
    threadId,
  );
  const acceptanceCriteria = parseWorkOrderListInput(
    draft?.acceptanceCriteriaInput,
  );
  const definitionOfDone = parseWorkOrderListInput(
    draft?.definitionOfDoneInput,
  );

  if (!threadId) {
    addError("thread_id", "thread_id is required.");
  }

  if (!objective) {
    addError("objective", "Objective is required.");
  }

  if (constraints.length === 0) {
    addError("constraints", "At least one constraint is required.");
  }

  if (acceptanceCriteria.length === 0) {
    addError(
      "acceptance_criteria",
      "At least one acceptance criterion is required.",
    );
  }

  if (definitionOfDone.length === 0) {
    addError(
      "definition_of_done",
      "At least one definition-of-done item is required.",
    );
  }

  const typedRefValidation = validateTypedRefs(contextRefs);
  if (!typedRefValidation.valid) {
    addError(
      "context_refs",
      `Invalid typed refs in context_refs: ${typedRefValidation.invalidRefs.join(", ")}`,
    );
  }

  return {
    valid: errors.length === 0,
    errors,
    fieldErrors,
    normalized: {
      thread_id: threadId,
      objective,
      constraints,
      context_refs: contextRefs,
      acceptance_criteria: acceptanceCriteria,
      definition_of_done: definitionOfDone,
    },
  };
}
