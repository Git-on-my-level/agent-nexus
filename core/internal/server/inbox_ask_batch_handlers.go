// Atomic batch respond for filed ask batches: the store surface, dispatch
// from single-item respond, request hashing, the multi-member respond
// handler, and authorized replay of an already-answered batch.

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// askBatchRespondStore is the store surface the atomic batch respond path needs.
type askBatchRespondStore interface {
	AppendHumanAttentionBatchResponse(context.Context, string, string, string, string, string, []primitives.HumanAttentionBatchResponseMember, ...map[string]any) (map[string]any, bool, error)
	HumanAttentionBatchResponseReplay(context.Context, string, string, string) (map[string]any, error)
	HumanAttentionBatchResponseClaimed(context.Context, string) (bool, error)
	HumanAttentionBatchResponseRequest(context.Context, string) (map[string]any, error)
}

// respondInboxBatchIfNeeded intercepts responses to a batch inbox item, which
// answer every member in one atomic pass. It also replays a retried batch
// submit whose item has already left the open projection. It returns true when
// the request was handled (or failed) as a batch response.
func respondInboxBatchIfNeeded(w http.ResponseWriter, r *http.Request, opts handlerOptions, effectiveItemID string, actorID string, req inboxRespondRequest) bool {
	if key, ok := strings.CutPrefix(effectiveItemID, primitives.CurationBatchInboxPrefix); ok {
		handleRespondCurationBatch(w, r, opts, key, effectiveItemID, actorID, req)
		return true
	}
	item, err := resolveInboxItemByVariants(r.Context(), opts.primitiveStore, effectiveItemID)
	batchStore, hasBatchStore := opts.primitiveStore.(askBatchRespondStore)
	if err == nil {
		payload := payloadFromDerivedInboxItem(item)
		if _, isBatch := payload["batch"].(map[string]any); !isBatch || !hasBatchStore {
			return false
		}
		handleRespondInboxBatch(w, r, opts, batchStore, item, payload, effectiveItemID, actorID, req)
		return true
	}
	if !errors.Is(err, primitives.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox item")
		return true
	}
	// An answered batch item is gone from the open projection. A retried
	// submit must replay through the batch claim before the single-ask path
	// reports the item missing.
	if !hasBatchStore {
		return false
	}
	hash := ""
	if req.IdempotencyKey != "" {
		var hashErr error
		hash, hashErr = hashBatchRespondRequest(actorID, effectiveItemID, req)
		if hashErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to hash human attention response")
			return true
		}
		replay, replayErr := batchStore.HumanAttentionBatchResponseReplay(r.Context(), actorID, req.IdempotencyKey, hash)
		if replayErr == nil {
			writeAuthorizedInboxBatchReplay(w, r, opts, replay)
			return true
		}
		if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return true
		}
		if !errors.Is(replayErr, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load response replay")
			return true
		}
	}
	// A no-key retry of an answered batch has nothing to replay through; the
	// stored batch claim (by item id) must still report the conflict instead
	// of falling into the single-ask path, which would 400 on the empty flat
	// fields before it ever saw the claim. Authorization still runs first so
	// an inaccessible answered batch looks absent, matching the single-ask path.
	if claimed, claimErr := batchStore.HumanAttentionBatchResponseRequest(r.Context(), effectiveItemID); claimErr == nil && claimed != nil {
		if !inboxBatchResponseAccessible(r, opts, claimed) {
			denyPMNotFound(w, "inbox item")
			return true
		}
		writeError(w, http.StatusConflict, "conflict", "this ask batch has already been answered")
		return true
	} else if !errors.Is(claimErr, primitives.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to check batch response state")
		return true
	}
	return false
}

func hashBatchRespondRequest(actorID, inboxItemID string, req inboxRespondRequest) (string, error) {
	hashInput, err := json.Marshal(struct {
		ActorID     string              `json:"actor_id"`
		InboxItemID string              `json:"inbox_item_id"`
		Request     inboxRespondRequest `json:"request"`
	}{actorID, inboxItemID, req})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(hashInput)
	return hex.EncodeToString(digest[:]), nil
}

// handleRespondInboxBatch records one answer per member of a filed ask batch
// in a single atomic pass. Each answer releases to its own ask's delivery
// mode: members with delivery=none stay recorded without waking the requester,
// the rest queue into the requester's debounced answer wake.
func handleRespondInboxBatch(
	w http.ResponseWriter,
	r *http.Request,
	opts handlerOptions,
	batchStore askBatchRespondStore,
	item primitives.DerivedInboxItem,
	itemPayload map[string]any,
	effectiveItemID string,
	actorID string,
	req inboxRespondRequest,
) {
	// Authorization precedes every batch-shape validation, mirroring the
	// single-ask invariant: an inaccessible batch must also look absent.
	batchThreadID := strings.TrimSpace(item.ThreadID)
	if batchThreadID == "" {
		batchThreadID = strings.TrimSpace(anyString(itemPayload["thread_id"]))
	}
	if !inboxItemAccessible(r, opts, batchThreadID, itemPayload) {
		denyPMNotFound(w, "inbox item")
		return
	}
	if len(req.IdempotencyKey) > 128 || strings.TrimSpace(req.IdempotencyKey) != req.IdempotencyKey {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotency_key must be at most 128 characters without surrounding whitespace")
		return
	}
	// A flat response_text answers every open member with the same text,
	// outcome and notify mode. The per-member answers array stays for the
	// inbox form, which rejects the flat fields.
	uniformText := strings.TrimSpace(req.ResponseText)
	uniform := len(req.Answers) == 0 && uniformText != ""
	if uniform {
		switch strings.TrimSpace(req.NotifyMode) {
		case "", "none", "original":
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", "a flat batch answer accepts notify_mode none or original")
			return
		}
		if strings.TrimSpace(req.NotifyTargetActorID) != "" || strings.TrimSpace(req.NotifyTargetAgentID) != "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "a flat batch answer does not take a replacement notify target")
			return
		}
	}
	if !uniform {
		if strings.TrimSpace(req.ResponseText) != "" || strings.TrimSpace(req.Outcome) != "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "response_text and outcome must be empty when answers are provided for a batch inbox item")
			return
		}
		if strings.TrimSpace(req.NotifyMode) != "" || strings.TrimSpace(req.NotifyTargetActorID) != "" || strings.TrimSpace(req.NotifyTargetAgentID) != "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "batch answers release to each ask's delivery mode; notify overrides are not supported")
			return
		}
		if len(req.Answers) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "answers is required for a batch inbox item")
			return
		}
	} else if err := schema.ValidateEnum(opts.contract, "human_attention_response_outcome", req.Outcome); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	batch, _ := itemPayload["batch"].(map[string]any)
	batchID := strings.TrimSpace(anyString(batch["id"]))
	if batchID == "" {
		writeError(w, http.StatusInternalServerError, "internal_error", "batch inbox item is missing batch id")
		return
	}
	sections := batchSectionsFromPayload(batch["asks"])
	if len(sections) == 0 {
		writeError(w, http.StatusInternalServerError, "internal_error", "batch inbox item has no decision sections")
		return
	}
	threadID := batchThreadID
	if !uniform && len(req.Answers) != len(sections) {
		writeError(w, http.StatusBadRequest, "invalid_request", "one answer per batch ask is required")
		return
	}

	// Answers are matched to members by ask ref so a stale section order
	// cannot shuffle a decision onto the wrong ask. A flat response fills
	// every section with that one text before the match.
	answerByRef := make(map[string]inboxRespondAnswer, len(sections))
	if uniform {
		for _, section := range sections {
			ref := strings.TrimSpace(anyString(section["request_event_ref"]))
			if ref == "" {
				writeError(w, http.StatusInternalServerError, "internal_error", "batch section is missing its request event ref")
				return
			}
			answerByRef[ref] = inboxRespondAnswer{AskRef: ref, ResponseText: uniformText, Outcome: req.Outcome, RelatedRefs: req.RelatedRefs}
		}
	}
	for _, answer := range req.Answers {
		ref := strings.TrimSpace(answer.AskRef)
		if ref == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "every answer needs ask_ref")
			return
		}
		if _, dup := answerByRef[ref]; dup {
			writeError(w, http.StatusBadRequest, "invalid_request", "duplicate answers for "+ref)
			return
		}
		answerByRef[ref] = answer
	}
	kind := canonicalHumanAttentionKind(anyString(itemPayload["kind"]))
	requesterActorID := strings.TrimSpace(anyString(itemPayload["requester_actor_id"]))
	requesterAgentID := strings.TrimSpace(anyString(itemPayload["requester_agent_id"]))
	requesterLabel := strings.TrimSpace(anyString(itemPayload["requester_label"]))

	var hygiene markdownHygieneCollector
	anyNotify := false
	members := make([]primitives.HumanAttentionBatchResponseMember, 0, len(sections))
	for _, section := range sections {
		ref := strings.TrimSpace(anyString(section["request_event_ref"]))
		answer, ok := answerByRef[ref]
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "missing answer for batch ask "+ref)
			return
		}
		delete(answerByRef, ref)
		sourceEventID := strings.TrimPrefix(ref, "event:")
		if sourceEventID == "" || !strings.HasPrefix(ref, "event:") {
			writeError(w, http.StatusInternalServerError, "internal_error", "batch section is missing its request event ref")
			return
		}
		answerText := strings.TrimSpace(answer.ResponseText)
		if answerText == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "response_text is required for batch ask "+ref)
			return
		}
		if err := schema.ValidateEnum(opts.contract, "human_attention_response_outcome", answer.Outcome); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "batch ask "+ref+": "+err.Error())
			return
		}
		if !hygiene.normalizeField(w, "response_text", &answerText) {
			return
		}
		answerText = strings.TrimSpace(answerText)
		answerRefs := normalizeStringSlice(answer.RelatedRefs)
		// The section's own subject is part of this answer's access surface:
		// a member whose card is restricted must not be answerable through
		// the batch item by a principal who cannot see that card.
		mergedRefs := append(stringSliceAny(itemPayload["related_refs"]), answerRefs...)
		mergedRefs = append(mergedRefs, strings.TrimSpace(anyString(section["subject_ref"])))
		responseAccess := cloneWorkspaceMap(itemPayload)
		responseAccess["related_refs"] = mergedRefs
		if !inboxItemAccessible(r, opts, threadID, responseAccess) {
			denyPMNotFound(w, "inbox item")
			return
		}
		memberDelivery := strings.TrimSpace(anyString(section["delivery"]))
		if memberDelivery == "" {
			memberDelivery = askDeliveryModeFromSourceEvent(r.Context(), opts, sourceEventID)
		}
		memberNotify := memberDelivery != "none"
		flatSilence := uniform && strings.EqualFold(strings.TrimSpace(req.NotifyMode), "none")
		if flatSilence {
			memberNotify = false
		}
		anyNotify = anyNotify || memberNotify
		summary := buildHumanAttentionResponseSummary(kind, map[string]any{"title": anyString(section["title"])}, answerText)
		responseRefs := mergeUniqueSortedRefs(append([]string{"thread:" + threadID, "inbox:" + effectiveItemID, "event:" + sourceEventID, ref, strings.TrimSpace(anyString(section["subject_ref"]))}, answerRefs...)...)
		event := map[string]any{
			"type":      humanAttentionRespondedEventType,
			"thread_id": threadID,
			"refs":      responseRefs,
			"summary":   summary,
			"payload": map[string]any{
				"inbox_item_id":       effectiveItemID,
				"request_event_ref":   ref,
				"kind":                kind,
				"response_text":       answerText,
				"outcome":             answer.Outcome,
				"subject_ref":         strings.TrimSpace(anyString(section["subject_ref"])),
				"related_refs":        responseRefs,
				"requester_actor_id":  requesterActorID,
				"requester_agent_id":  requesterAgentID,
				"requester_label":     requesterLabel,
				"responding_actor_id": actorID,
				"batch_id":            batchID,
			},
			"provenance": eventProvenance(),
		}
		if err := validateEventReferenceConventions(opts.contract, event, responseRefs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "batch ask "+ref+": "+err.Error())
			return
		}
		notifyMessageText := "Answer wake queued for the requester."
		if !memberNotify {
			notifyMessageText = "Ask requested no delivery (mode none); answer recorded without waking the requester."
			if flatSilence {
				notifyMessageText = "Response recorded without notification target."
			}
		}
		members = append(members, primitives.HumanAttentionBatchResponseMember{
			SourceEventID: sourceEventID,
			InboxItemID:   effectiveItemID,
			Event:         event,
			Notify: map[string]any{
				"requested":       memberNotify,
				"queued":          memberNotify,
				"message":         notifyMessageText,
				"target_actor_id": requesterActorID,
				"target_agent_id": requesterAgentID,
				"workspace_id":    opts.workspaceID,
				"thread_id":       threadID,
				"subject_ref":     strings.TrimSpace(anyString(section["subject_ref"])),
				"mode":            "original",
				"quiet_window_ns": int64(opts.answerWakeQuietWindow),
			},
		})
	}
	if len(answerByRef) != 0 {
		unknownRef := ""
		for ref := range answerByRef {
			unknownRef = ref
			break
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "answer ask_ref "+unknownRef+" is not part of this batch")
		return
	}

	var target humanAttentionResponseTarget
	if anyNotify {
		resolved, found, targetErr := resolveAgentNotificationTarget(r.Context(), opts, requesterActorID, requesterAgentID)
		if targetErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve requester notification target")
			return
		}
		if !found {
			writeError(w, http.StatusConflict, "notification_target_required", "original requester is not resolvable; the batch cannot release its wakes")
			return
		}
		target = resolved
		target.Mode = "original"
	} else {
		target = humanAttentionResponseTarget{Mode: "none"}
	}

	hash, err := hashBatchRespondRequest(actorID, effectiveItemID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to hash human attention response")
		return
	}
	if req.IdempotencyKey != "" {
		replay, replayErr := batchStore.HumanAttentionBatchResponseReplay(r.Context(), actorID, req.IdempotencyKey, hash)
		if replayErr == nil {
			writeAuthorizedInboxBatchReplay(w, r, opts, replay)
			return
		}
		if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return
		}
		if !errors.Is(replayErr, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load response replay")
			return
		}
	}
	claimed, claimErr := batchStore.HumanAttentionBatchResponseClaimed(r.Context(), batchID)
	if claimErr != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to check batch response state")
		return
	}
	if claimed {
		if req.IdempotencyKey != "" {
			replay, replayErr := batchStore.HumanAttentionBatchResponseReplay(r.Context(), actorID, req.IdempotencyKey, hash)
			if replayErr == nil {
				writeAuthorizedInboxBatchReplay(w, r, opts, replay)
				return
			}
			if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
				writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
				return
			}
		}
		writeError(w, http.StatusConflict, "conflict", "this ask batch has already been answered")
		return
	}

	notifyMessageText := "All batch answers recorded; wakes queue per ask delivery."
	if !anyNotify {
		notifyMessageText = "Every ask in this batch requested no delivery (mode none); answers recorded without waking the requester."
		if uniform && strings.EqualFold(strings.TrimSpace(req.NotifyMode), "none") {
			notifyMessageText = "Response recorded without notification target."
		}
	}
	notify := map[string]any{
		"requested":       anyNotify,
		"queued":          anyNotify,
		"message":         notifyMessageText,
		"target_actor_id": target.ActorID,
		"target_agent_id": target.AgentID,
		"target_handle":   target.Handle,
		"mode":            target.Mode,
		"batch_id":        batchID,
	}
	responseFields := hygiene.attach(map[string]any{"notify": notify})
	stored, replayed, err := batchStore.AppendHumanAttentionBatchResponse(r.Context(), actorID, batchID, effectiveItemID, req.IdempotencyKey, hash, members, responseFields)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrInvalidAccessDecision) || errors.Is(err, auth.ErrInvalidRequest) || errors.Is(err, auth.ErrAgentNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, auth.ErrHumanRequired) {
			writeError(w, http.StatusForbidden, "human_required", "only active humans can decide access requests")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) {
			writeError(w, http.StatusConflict, "conflict", "this ask batch has already been answered")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionMemberWithdrawn) {
			// Nothing committed: the reader refreshes the item and re-submits
			// against the shrunk batch.
			writeError(w, http.StatusConflict, "conflict", "batch member "+err.Error()+" was withdrawn; refresh the item and answer the remaining asks")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to store batch response")
		return
	}
	if replayed {
		writeAuthorizedInboxBatchReplay(w, r, opts, stored)
		return
	}

	notifyProjectionMaintainer(opts)
	writeJSON(w, http.StatusCreated, stored)
}

// writeAuthorizedInboxBatchReplay returns a stored batch response under
// current authorization: the answered batch item is gone from the open
// projection, so the stored answer events carry the original subject/refs.
func writeAuthorizedInboxBatchReplay(w http.ResponseWriter, r *http.Request, opts handlerOptions, replay map[string]any) {
	if !inboxBatchResponseAccessible(r, opts, replay) {
		denyPMNotFound(w, "inbox item")
		return
	}
	writeJSON(w, http.StatusCreated, replay)
}

func inboxBatchResponseAccessible(r *http.Request, opts handlerOptions, replay map[string]any) bool {
	answers, _ := replay["answers"].([]any)
	if len(answers) == 0 {
		return false
	}
	for _, raw := range answers {
		answer, _ := raw.(map[string]any)
		event, _ := answer["event"].(map[string]any)
		if event == nil {
			return false
		}
		if !inboxItemAccessible(r, opts, eventThreadID(event), event) {
			return false
		}
	}
	return true
}

func batchSectionsFromPayload(raw any) []map[string]any {
	out := []map[string]any{}
	switch typed := raw.(type) {
	case []map[string]any:
		for _, section := range typed {
			if section != nil {
				out = append(out, section)
			}
		}
	case []any:
		for _, entry := range typed {
			if section, ok := entry.(map[string]any); ok && section != nil {
				out = append(out, section)
			}
		}
	}
	return out
}
