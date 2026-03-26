import { parseListInput, validateTypedRefs } from "./typedRefs.js";

const ALLOWED_REVIEW_OUTCOMES = new Set(["accept", "revise", "escalate"]);

export function validateReviewDraft(draft, options = {}) {
  const threadId = String(options.threadId ?? "").trim();
  const receiptId = String(options.receiptId ?? "").trim();
  const workOrderId = String(options.workOrderId ?? "").trim();
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

  if (!threadId) {
    addError("thread_id", "thread_id is required.");
  }

  if (!receiptId) {
    addError("receipt_id", "receipt_id is required.");
  }

  if (!workOrderId) {
    addError("work_order_id", "work_order_id is required.");
  }

  if (!reviewId) {
    addError("review_id", "review_id is required.");
  }

  if (!ALLOWED_REVIEW_OUTCOMES.has(outcome)) {
    addError("outcome", "outcome must be one of: accept, revise, escalate.");
  }

  if (!notes) {
    addError("notes", "notes is required.");
  }

  const evidenceValidation = validateTypedRefs(evidenceRefs);
  if (!evidenceValidation.valid) {
    addError(
      "evidence_refs",
      `Invalid typed refs in evidence_refs: ${evidenceValidation.invalidRefs.join(", ")}`,
    );
  }

  return {
    valid: errors.length === 0,
    errors,
    fieldErrors,
    normalized: {
      review_id: reviewId,
      receipt_id: receiptId,
      work_order_id: workOrderId,
      thread_id: threadId,
      outcome,
      notes,
      evidence_refs: evidenceRefs,
    },
  };
}

export function buildReviewPayload(draft, options = {}) {
  const validation = validateReviewDraft(draft, options);
  if (!validation.valid) {
    return {
      ...validation,
      packet: null,
      artifact: null,
    };
  }

  const packet = {
    review_id: validation.normalized.review_id,
    work_order_id: validation.normalized.work_order_id,
    receipt_id: validation.normalized.receipt_id,
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
      id: validation.normalized.review_id,
      kind: "review",
      thread_id: validation.normalized.thread_id,
      summary: `Review (${validation.normalized.outcome}) for ${validation.normalized.receipt_id}`,
      refs: [
        `thread:${validation.normalized.thread_id}`,
        `artifact:${validation.normalized.receipt_id}`,
        `artifact:${validation.normalized.work_order_id}`,
      ],
    },
  };
}
