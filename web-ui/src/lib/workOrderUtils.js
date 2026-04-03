import {
  parseRef,
  parseListInput,
  serializeListInput,
  validateTypedRefs,
} from "./typedRefs.js";

export function ensureThreadRef(refs = [], threadId) {
  const normalized = refs.map((item) => String(item).trim()).filter(Boolean);
  const threadRef = `thread:${threadId}`;

  if (!normalized.includes(threadRef)) {
    normalized.unshift(threadRef);
  }

  return normalized;
}

function dedupePreserveOrder(items = []) {
  const out = [];
  const seen = new Set();

  items.forEach((item) => {
    const normalized = String(item ?? "").trim();
    if (!normalized || seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    out.push(normalized);
  });

  return out;
}

function normalizeArtifactLikeRef(rawValue) {
  const normalized = String(rawValue ?? "").trim();
  if (!normalized) return "";

  const parsed = parseRef(normalized);
  if (parsed.prefix && parsed.value) {
    return normalized;
  }

  return `artifact:${normalized}`;
}

function normalizeEventRef(eventId) {
  const normalized = String(eventId ?? "").trim();
  if (!normalized) return "";
  return `event:${normalized}`;
}

function normalizeDocumentRef(documentId) {
  const normalized = String(documentId ?? "").trim();
  if (!normalized) return "";
  return `document:${normalized}`;
}

function suggestionTimestamp(value) {
  const parsed = Date.parse(String(value ?? ""));
  return Number.isFinite(parsed) ? parsed : 0;
}

function addSuggestion(suggestions, seenRefs, nextSuggestion) {
  const ref = String(nextSuggestion?.ref ?? "").trim();
  if (!ref || seenRefs.has(ref)) {
    return;
  }

  seenRefs.add(ref);
  suggestions.push({
    ref,
    kind: String(nextSuggestion.kind ?? "").trim() || "context",
    source: String(nextSuggestion.source ?? "").trim() || "Context",
    title: String(nextSuggestion.title ?? "").trim() || ref,
    detail: String(nextSuggestion.detail ?? "").trim(),
  });
}

export function buildWorkOrderContextSuggestions({
  snapshot = {},
  documents = [],
  timeline = [],
} = {}) {
  const suggestions = [];
  const seenRefs = new Set();

  const keyArtifacts = Array.isArray(snapshot?.key_artifacts)
    ? snapshot.key_artifacts
    : [];
  keyArtifacts.forEach((rawRef) => {
    const ref = normalizeArtifactLikeRef(rawRef);
    if (!ref) return;
    addSuggestion(suggestions, seenRefs, {
      ref,
      kind: "artifact",
      source: "Key artifact",
      title: ref,
    });
  });

  const recentEvents = [...(Array.isArray(timeline) ? timeline : [])].sort(
    (left, right) =>
      suggestionTimestamp(right?.ts ?? right?.created_at) -
      suggestionTimestamp(left?.ts ?? left?.created_at),
  );
  recentEvents.forEach((event) => {
    const type = String(event?.type ?? "").trim();
    if (type === "receipt_added" || type === "review_completed") {
      const artifactRef = normalizeArtifactLikeRef(event?.payload?.artifact_id);
      if (!artifactRef) return;
      addSuggestion(suggestions, seenRefs, {
        ref: artifactRef,
        kind: "artifact",
        source: type === "receipt_added" ? "Recent receipt" : "Recent review",
        title: String(event?.summary ?? "").trim() || artifactRef,
        detail: String(event?.ts ?? event?.created_at ?? "").trim(),
      });
      return;
    }

    if (
      type === "decision_needed" ||
      type === "intervention_needed" ||
      type === "decision_made"
    ) {
      const eventRef = normalizeEventRef(event?.id);
      if (!eventRef) return;
      addSuggestion(suggestions, seenRefs, {
        ref: eventRef,
        kind: "event",
        source:
          type === "decision_needed"
            ? "Pending decision"
            : type === "intervention_needed"
              ? "Pending intervention"
              : "Recent decision",
        title: String(event?.summary ?? "").trim() || eventRef,
        detail: String(event?.ts ?? event?.created_at ?? "").trim(),
      });
    }
  });

  (Array.isArray(documents) ? documents : []).forEach((document) => {
    const ref = normalizeDocumentRef(document?.id);
    if (!ref) return;

    const revisionNumber = Number(document?.head_revision?.revision_number);
    const detailParts = [];
    if (String(document?.status ?? "").trim()) {
      detailParts.push(String(document.status).trim());
    }
    if (Number.isFinite(revisionNumber) && revisionNumber > 0) {
      detailParts.push(`v${revisionNumber}`);
    }
    if (String(document?.head_revision?.content_type ?? "").trim()) {
      detailParts.push(String(document.head_revision.content_type).trim());
    }

    addSuggestion(suggestions, seenRefs, {
      ref,
      kind: "document",
      source: "Thread document",
      title: String(document?.title ?? "").trim() || ref,
      detail: detailParts.join(" • "),
    });
  });

  return suggestions;
}

export function mergeContextRefsInput(rawInput, refsToAdd = [], options = {}) {
  const threadId = String(options.threadId ?? "").trim();
  const currentRefs = parseListInput(rawInput);
  const merged = dedupePreserveOrder([...currentRefs, ...refsToAdd]);

  return serializeListInput(
    threadId ? ensureThreadRef(merged, threadId) : merged,
  );
}

export function removeContextRefsFromInput(
  rawInput,
  refsToRemove = [],
  options = {},
) {
  const threadId = String(options.threadId ?? "").trim();
  const removeSet = new Set(
    refsToRemove.map((item) => String(item ?? "").trim()).filter(Boolean),
  );
  const remaining = parseListInput(rawInput).filter(
    (item) => !removeSet.has(String(item).trim()),
  );

  return serializeListInput(
    threadId ? ensureThreadRef(remaining, threadId) : remaining,
  );
}

export function applyWorkOrderContextPrefill({
  currentInput = "",
  threadId = "",
  prefillRefs = [],
  prefillKey = "",
  appliedPrefillKey = "",
} = {}) {
  const normalizedPrefillKey = String(prefillKey ?? "").trim();
  if (!normalizedPrefillKey || normalizedPrefillKey === appliedPrefillKey) {
    return {
      applied: false,
      nextInput: String(currentInput ?? ""),
      nextAppliedPrefillKey: String(appliedPrefillKey ?? ""),
    };
  }

  return {
    applied: true,
    nextInput: mergeContextRefsInput(currentInput, prefillRefs, { threadId }),
    nextAppliedPrefillKey: normalizedPrefillKey,
  };
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
  const constraints = parseListInput(draft?.constraintsInput);
  const contextRefs = ensureThreadRef(
    parseListInput(draft?.contextRefsInput),
    threadId,
  );
  const acceptanceCriteria = parseListInput(draft?.acceptanceCriteriaInput);
  const definitionOfDone = parseListInput(draft?.definitionOfDoneInput);

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
