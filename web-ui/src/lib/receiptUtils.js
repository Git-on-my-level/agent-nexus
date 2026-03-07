import { parseRef } from "./typedRefs.js";

export function parseReceiptListInput(rawValue) {
  return String(rawValue ?? "")
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function serializeReceiptListInput(items) {
  if (!Array.isArray(items)) {
    return "";
  }

  return items
    .map((item) => String(item).trim())
    .filter(Boolean)
    .join("\n");
}

export function validateReceiptTypedRefs(refs = []) {
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

export function validateReceiptDraft(draft, options = {}) {
  const threadId = String(options.threadId ?? "").trim();
  const errors = [];
  const fieldErrors = {};

  function addError(field, message) {
    errors.push(message);
    if (!fieldErrors[field]) fieldErrors[field] = [];
    fieldErrors[field].push(message);
  }

  const workOrderId = String(draft?.workOrderId ?? "").trim();
  const outputs = parseReceiptListInput(draft?.outputsInput);
  const verificationEvidence = parseReceiptListInput(
    draft?.verificationEvidenceInput,
  );
  const changesSummary = String(draft?.changesSummary ?? "").trim();
  const knownGaps = parseReceiptListInput(draft?.knownGapsInput);

  if (!threadId) {
    addError("thread_id", "thread_id is required.");
  }

  if (!workOrderId) {
    addError("work_order_id", "work_order_id is required.");
  }

  if (!changesSummary) {
    addError("changes_summary", "changes_summary is required.");
  }

  if (outputs.length === 0) {
    addError("outputs", "outputs must include at least one typed ref.");
  }

  if (verificationEvidence.length === 0) {
    addError(
      "verification_evidence",
      "verification_evidence must include at least one typed ref.",
    );
  }

  const outputRefValidation = validateReceiptTypedRefs(outputs);
  if (!outputRefValidation.valid) {
    addError(
      "outputs",
      `Invalid typed refs in outputs: ${outputRefValidation.invalidRefs.join(", ")}`,
    );
  }

  const evidenceRefValidation = validateReceiptTypedRefs(verificationEvidence);
  if (!evidenceRefValidation.valid) {
    addError(
      "verification_evidence",
      `Invalid typed refs in verification_evidence: ${evidenceRefValidation.invalidRefs.join(", ")}`,
    );
  }

  return {
    valid: errors.length === 0,
    errors,
    fieldErrors,
    normalized: {
      thread_id: threadId,
      work_order_id: workOrderId,
      outputs,
      verification_evidence: verificationEvidence,
      changes_summary: changesSummary,
      known_gaps: knownGaps,
    },
  };
}
