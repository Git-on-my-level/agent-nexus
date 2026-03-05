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
    errors.push("thread_id is required.");
  }

  if (!objective) {
    errors.push("Objective is required.");
  }

  if (constraints.length === 0) {
    errors.push("At least one constraint is required.");
  }

  if (acceptanceCriteria.length === 0) {
    errors.push("At least one acceptance criterion is required.");
  }

  if (definitionOfDone.length === 0) {
    errors.push("At least one definition-of-done item is required.");
  }

  const typedRefValidation = validateTypedRefs(contextRefs);
  if (!typedRefValidation.valid) {
    errors.push(
      `Invalid typed refs in context_refs: ${typedRefValidation.invalidRefs.join(", ")}`,
    );
  }

  return {
    valid: errors.length === 0,
    errors,
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
