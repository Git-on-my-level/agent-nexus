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

  const workOrderId = String(draft?.workOrderId ?? "").trim();
  const outputs = parseListInput(draft?.outputsInput);
  const verificationEvidence = parseListInput(draft?.verificationEvidenceInput);
  const changesSummary = String(draft?.changesSummary ?? "").trim();
  const knownGaps = parseListInput(draft?.knownGapsInput);

  if (!subjectRef) {
    addError("subject_ref", "subject_ref is required.");
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

  const workOrderRef = workOrderId ? `artifact:${workOrderId}` : "";

  return {
    valid: errors.length === 0,
    errors,
    fieldErrors,
    normalized: {
      subject_ref: subjectRef,
      work_order_id: workOrderId,
      work_order_ref: workOrderRef,
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
    work_order_ref: validation.normalized.work_order_ref,
    outputs: validation.normalized.outputs,
    verification_evidence: validation.normalized.verification_evidence,
    changes_summary: validation.normalized.changes_summary,
    known_gaps: validation.normalized.known_gaps,
  };

  return {
    valid: true,
    errors: [],
    fieldErrors: validation.fieldErrors,
    normalized: validation.normalized,
    packet,
    artifact: {
      ...(receiptId ? { id: receiptId } : {}),
      kind: "receipt",
      summary: `Receipt for ${validation.normalized.work_order_id}`,
      refs: [
        validation.normalized.subject_ref,
        validation.normalized.work_order_ref,
      ],
    },
  };
}
