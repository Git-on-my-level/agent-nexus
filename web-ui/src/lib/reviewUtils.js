import { parseListInput, validateTypedRefs } from "./typedRefs.js";

const ALLOWED_REVIEW_OUTCOMES = new Set(["accept", "revise", "escalate"]);

export function validateReviewDraft(draft, options = {}) {
  const subjectRef = String(options.subjectRef ?? "").trim();
  const receiptId = String(options.receiptId ?? "").trim();
  const receiptRefOpt = String(options.receiptRef ?? "").trim();
  const reviewId = String(options.reviewId ?? "").trim();

  const errors = [];
  const fieldErrors = {};

  function addError(field, message) {
    errors.push(message);
    if (!fieldErrors[field]) fieldErrors[field] = [];
    fieldErrors[field].push(message);
  }

  const outcome = String(draft?.outcome ?? "").trim();
  const notes = String(draft?.notes ?? "").trim();
  const evidenceRefs = parseListInput(draft?.evidenceRefsInput);

  const receiptRef =
    receiptRefOpt || (receiptId ? `artifact:${receiptId}` : "");

  if (!subjectRef) {
    addError("subject_ref", "subject_ref is required.");
  } else if (!subjectRef.startsWith("card:")) {
    addError("subject_ref", "subject_ref must be a card ref (card:...).");
  }

  if (!receiptRef) {
    addError("receipt_id", "receipt_ref or receipt_id is required.");
  }

  if (!ALLOWED_REVIEW_OUTCOMES.has(outcome)) {
    addError("outcome", "outcome must be one of: accept, revise, escalate.");
  }

  if (!notes) {
    addError("notes", "notes is required.");
  }

  if (evidenceRefs.length === 0) {
    addError(
      "evidence_refs",
      "evidence_refs must include at least one typed ref.",
    );
  }

  const evidenceValidation = validateTypedRefs(evidenceRefs);
  if (!evidenceValidation.valid) {
    addError(
      "evidence_refs",
      `Invalid typed refs in evidence_refs: ${evidenceValidation.invalidRefs.join(", ")}`,
    );
  }

  const resolvedReceiptId = receiptRef.startsWith("artifact:")
    ? receiptRef.slice("artifact:".length).trim()
    : receiptId;

  return {
    valid: errors.length === 0,
    errors,
    fieldErrors,
    normalized: {
      review_id: reviewId,
      receipt_id: resolvedReceiptId,
      receipt_ref: receiptRef,
      subject_ref: subjectRef,
      outcome,
      notes,
      evidence_refs: evidenceRefs,
    },
  };
}

export function buildReviewPayload(draft, options = {}) {
  const reviewId =
    String(options.reviewId ?? "").trim() ||
    `rv-${Math.random().toString(36).slice(2, 10)}`;
  const validation = validateReviewDraft(draft, { ...options, reviewId });
  if (!validation.valid) {
    return {
      ...validation,
      packet: null,
      artifact: null,
    };
  }

  const packet = {
    review_id: reviewId,
    subject_ref: validation.normalized.subject_ref,
    receipt_ref: validation.normalized.receipt_ref,
    outcome: validation.normalized.outcome,
    notes: validation.normalized.notes,
    evidence_refs: validation.normalized.evidence_refs,
  };

  return {
    valid: true,
    errors: [],
    fieldErrors: validation.fieldErrors,
    normalized: validation.normalized,
    packet,
    artifact: {
      id: reviewId,
      kind: "review",
      summary: `Review (${validation.normalized.outcome}) for ${validation.normalized.receipt_id}`,
      refs: [
        validation.normalized.subject_ref,
        validation.normalized.receipt_ref,
      ],
    },
  };
}
