// Responding to an accepted curation batch: one response_text, outcome and
// notify mode answering every open member in a single transaction.

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// handleRespondCurationBatch answers every open member of an accepted curation
// batch with one response_text, outcome and notify mode, in one transaction.
// A hidden open member makes the batch look absent.
func handleRespondCurationBatch(w http.ResponseWriter, r *http.Request, opts handlerOptions, key, effectiveItemID, actorID string, req inboxRespondRequest) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 128 || strings.TrimSpace(key) != key || strings.ContainsAny(key, " \t\r\n/") {
		writeError(w, http.StatusBadRequest, "invalid_request", "curation batch id is invalid")
		return
	}
	batchStore, ok := opts.primitiveStore.(askBatchRespondStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "human attention response store is not configured")
		return
	}
	loader, ok := opts.primitiveStore.(interface {
		OpenCurationBatchMembers(context.Context, string) ([]primitives.CurationBatchMemberAsk, bool, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "curation batch store is not configured")
		return
	}
	if len(req.Answers) > 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "a curation batch takes one response_text for every member")
		return
	}
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
	if len(req.IdempotencyKey) > 128 || strings.TrimSpace(req.IdempotencyKey) != req.IdempotencyKey {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotency_key must be at most 128 characters without surrounding whitespace")
		return
	}
	if err := schema.ValidateEnum(opts.contract, "human_attention_response_outcome", req.Outcome); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var hygiene markdownHygieneCollector
	responseText := strings.TrimSpace(req.ResponseText)
	if !hygiene.normalizeField(w, "response_text", &responseText) {
		return
	}
	responseText = strings.TrimSpace(responseText)
	if responseText == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "response_text is required")
		return
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
	members, hidden, err := loader.OpenCurationBatchMembers(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load curation batch")
		return
	}
	if hidden {
		denyPMNotFound(w, "inbox item")
		return
	}
	if len(members) == 0 {
		if claimed, claimErr := batchStore.HumanAttentionBatchResponseRequest(r.Context(), effectiveItemID); claimErr == nil && claimed != nil {
			if !inboxBatchResponseAccessible(r, opts, claimed) {
				denyPMNotFound(w, "inbox item")
				return
			}
			writeError(w, http.StatusConflict, "conflict", "this ask batch has already been answered")
			return
		} else if claimErr != nil && !errors.Is(claimErr, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to check batch response state")
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "Inbox item not found; your response can be retried")
		return
	}
	answerRefs := normalizeStringSlice(req.RelatedRefs)
	prepared := make([]primitives.HumanAttentionBatchResponseMember, 0, len(members))
	targets := map[string]humanAttentionResponseTarget{}
	anyNotify := false
	for _, member := range members {
		kind := canonicalHumanAttentionKind(member.Kind)
		if kind == "" {
			writeError(w, http.StatusInternalServerError, "internal_error", "curation batch member is not a human attention item")
			return
		}
		threadID := strings.TrimSpace(member.ThreadID)
		if threadID == "" || strings.TrimSpace(member.AskID) == "" || strings.TrimSpace(member.InboxItemID) == "" {
			writeError(w, http.StatusInternalServerError, "internal_error", "curation batch member is missing its inbox id")
			return
		}
		related := append([]string{}, answerRefs...)
		if strings.TrimSpace(member.SubjectRef) != "" {
			related = append(related, strings.TrimSpace(member.SubjectRef))
		}
		access := map[string]any{
			"kind": kind, "thread_id": threadID, "subject_ref": member.SubjectRef, "related_refs": related,
		}
		if !inboxItemAccessible(r, opts, threadID, access) {
			denyPMNotFound(w, "inbox item")
			return
		}
		delivery := strings.TrimSpace(member.Delivery)
		if delivery == "" {
			delivery = askDeliveryModeFromSourceEvent(r.Context(), opts, member.AskID)
		}
		target := humanAttentionResponseTarget{Mode: "none"}
		memberNotify := delivery != "none" && !strings.EqualFold(strings.TrimSpace(req.NotifyMode), "none")
		if memberNotify {
			cacheKey := member.RequesterActor + "\n" + member.RequesterAgent + "\n" + req.NotifyTargetActorID + "\n" + req.NotifyTargetAgentID
			cached, seen := targets[cacheKey]
			if !seen {
				resolved, ok := resolveHumanAttentionResponseTarget(w, r, opts, humanAttentionTargetRequest{
					NotifyMode: req.NotifyMode, NotifyTargetActorID: req.NotifyTargetActorID, NotifyTargetAgentID: req.NotifyTargetAgentID,
					RequesterActorID: member.RequesterActor, RequesterAgentID: member.RequesterAgent,
				})
				if !ok {
					return
				}
				targets[cacheKey] = resolved
				cached = resolved
			}
			target = cached
			memberNotify = target.Mode != "none"
		}
		anyNotify = anyNotify || memberNotify
		ref := "event:" + member.AskID
		responseRefs := mergeUniqueSortedRefs(append([]string{"thread:" + threadID, "inbox:" + member.InboxItemID, ref, strings.TrimSpace(member.SubjectRef)}, answerRefs...)...)
		summary := buildHumanAttentionResponseSummary(kind, map[string]any{"title": member.Title}, responseText)
		event := map[string]any{
			"type": humanAttentionRespondedEventType, "thread_id": threadID, "refs": responseRefs, "summary": summary,
			"payload": map[string]any{
				"inbox_item_id": member.InboxItemID, "request_event_ref": ref, "kind": kind,
				"response_text": responseText, "outcome": req.Outcome, "subject_ref": strings.TrimSpace(member.SubjectRef),
				"related_refs": responseRefs, "requester_actor_id": member.RequesterActor, "requester_agent_id": member.RequesterAgent,
				"requester_label": member.RequesterLabel, "responding_actor_id": actorID,
				"notified_actor_id": target.ActorID, "notified_agent_id": target.AgentID,
				"batch_id": primitives.CurationBatchClaimID(key),
			},
			"provenance": eventProvenance(),
		}
		if err := validateEventReferenceConventions(opts.contract, event, responseRefs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		notifyMessage := map[bool]string{true: "Answer wake queued for the requester.", false: "Response recorded without notification target."}[memberNotify]
		if delivery == "none" {
			notifyMessage = "Ask requested no delivery (mode none); response recorded without waking the requester."
		}
		prepared = append(prepared, primitives.HumanAttentionBatchResponseMember{
			SourceEventID: member.AskID, InboxItemID: member.InboxItemID, Event: event,
			Notify: map[string]any{
				"requested": memberNotify, "queued": memberNotify, "message": notifyMessage,
				"target_actor_id": target.ActorID, "target_agent_id": target.AgentID, "target_handle": target.Handle,
				"workspace_id": opts.workspaceID, "thread_id": threadID, "subject_ref": strings.TrimSpace(member.SubjectRef),
				"mode": target.Mode, "quiet_window_ns": int64(opts.answerWakeQuietWindow),
			},
		})
	}
	notifyMessage := "All batch answers recorded; wakes queue per ask delivery."
	mode := "original"
	var targetActor, targetAgent, targetHandle string
	if !anyNotify {
		notifyMessage = "Response recorded without notification target."
		mode = "none"
	} else if len(targets) == 1 {
		for _, target := range targets {
			mode = target.Mode
			targetActor, targetAgent, targetHandle = target.ActorID, target.AgentID, target.Handle
		}
	}
	notify := map[string]any{
		"requested": anyNotify, "queued": anyNotify, "message": notifyMessage,
		"target_actor_id": targetActor, "target_agent_id": targetAgent, "target_handle": targetHandle, "mode": mode,
		"batch_id": primitives.CurationBatchClaimID(key),
	}
	stored, replayed, err := batchStore.AppendHumanAttentionBatchResponse(r.Context(), actorID, primitives.CurationBatchClaimID(key), effectiveItemID, req.IdempotencyKey, hash, prepared, hygiene.attach(map[string]any{"notify": notify}))
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) {
			writeError(w, http.StatusConflict, "conflict", "this ask batch has already been answered")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionMemberWithdrawn) {
			writeError(w, http.StatusConflict, "conflict", "batch member "+err.Error()+" was withdrawn; refresh and answer the remaining asks")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return
		}
		if errors.Is(err, primitives.ErrInvalidAccessDecision) || errors.Is(err, auth.ErrInvalidRequest) || errors.Is(err, auth.ErrAgentNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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
