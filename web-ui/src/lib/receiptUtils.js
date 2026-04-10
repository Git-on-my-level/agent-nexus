import { parseListInput, validateTypedRefs } from "./typedRefs.js";

export function validateReceiptDraft(draft, options = {}) {
  const subjectRef = String(options.subjectRef ?? "").trim();
  const errors = [];
  const fieldErrors = {};

  function addError(field, message) {
    errors.push(message);
    if (!fieldErrors[field]) fieldErrors[field] = [];
    fieldErrors[field].push(message);
  }

  const outputs = parseListInput(draft?.outputsInput);
  const verificationEvidence = parseListInput(draft?.verificationEvidenceInput);
  const changesSummary = String(draft?.changesSummary ?? "").trim();
  const knownGaps = parseListInput(draft?.knownGapsInput);

  if (!subjectRef) {
    addError("subject_ref", "subject_ref is required.");
  } else if (!subjectRef.startsWith("card:")) {
    addError("subject_ref", "subject_ref must be a card ref (card:...).");
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

  const outputRefValidation = validateTypedRefs(outputs);
  if (!outputRefValidation.valid) {
    addError(
      "outputs",
      `Invalid typed refs in outputs: ${outputRefValidation.invalidRefs.join(", ")}`,
    );
  }

  const evidenceRefValidation = validateTypedRefs(verificationEvidence);
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
      subject_ref: subjectRef,
      outputs,
      verification_evidence: verificationEvidence,
      changes_summary: changesSummary,
      known_gaps: knownGaps,
    },
  };
}

export function buildReceiptPayload(draft, options = {}) {
  const validation = validateReceiptDraft(draft, options);
  if (!validation.valid) {
    return {
      ...validation,
      packet: null,
      artifact: null,
    };
  }

  const receiptId = String(options.receiptId ?? "").trim();
  const packet = {
    ...(receiptId ? { receipt_id: receiptId } : {}),
    subject_ref: validation.normalized.subject_ref,
    outputs: validation.normalized.outputs,
    verification_evidence: validation.normalized.verification_evidence,
    changes_summary: validation.normalized.changes_summary,
    known_gaps: validation.normalized.known_gaps,
  };

  const summarySlice = validation.normalized.changes_summary.slice(0, 120);

  return {
    valid: true,
    errors: [],
    fieldErrors: validation.fieldErrors,
    normalized: validation.normalized,
    packet,
    artifact: {
      ...(receiptId ? { id: receiptId } : {}),
      kind: "receipt",
      summary: summarySlice,
      refs: [validation.normalized.subject_ref],
    },
  };
}
