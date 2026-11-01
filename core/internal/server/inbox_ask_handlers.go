// HTTP handler for responding to a single inbox item (open asks, agent
// notifications, human attention items), including the request payload types
// it decodes. Batch respond flows live in inbox_ask_batch_handlers.go and
// inbox_ask_curation_batch.go; target resolution and wake in
// inbox_response_targets.go.

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

type humanAttentionResponseStore interface {
	HumanAttentionResponseRequest(context.Context, string) (map[string]any, error)
	HumanAttentionResponseReplay(context.Context, string, string, string) (map[string]any, error)
	HumanAttentionResponseClaimed(context.Context, string) (bool, error)
	AppendHumanAttentionResponse(context.Context, string, string, string, string, string, map[string]any, map[string]any, ...map[string]any) (map[string]any, bool, error)
}

type inboxRespondAnswer struct {
	AskRef       string   `json:"ask_ref"`
	ResponseText string   `json:"response_text"`
	Outcome      string   `json:"outcome"`
	RelatedRefs  []string `json:"related_refs"`
}

type inboxRespondRequest struct {
	IdempotencyKey      string               `json:"idempotency_key"`
	ActorID             string               `json:"actor_id"`
	InboxItemID         string               `json:"inbox_item_id"`
	ResponseText        string               `json:"response_text"`
	Outcome             string               `json:"outcome"`
	RelatedRefs         []string             `json:"related_refs"`
	NotifyMode          string               `json:"notify_mode"`
	NotifyTargetActorID string               `json:"notify_target_actor_id"`
	NotifyTargetAgentID string               `json:"notify_target_agent_id"`
	Answers             []inboxRespondAnswer `json:"answers"`
}

// inboxRespondRequestCore preserves the exact field order single-ask response
// requests have always hashed, so replays of requests sent before batch
// answers existed still match.
type inboxRespondRequestCore struct {
	IdempotencyKey      string   `json:"idempotency_key"`
	ActorID             string   `json:"actor_id"`
	InboxItemID         string   `json:"inbox_item_id"`
	ResponseText        string   `json:"response_text"`
	Outcome             string   `json:"outcome"`
	RelatedRefs         []string `json:"related_refs"`
	NotifyMode          string   `json:"notify_mode"`
	NotifyTargetActorID string   `json:"notify_target_actor_id"`
	NotifyTargetAgentID string   `json:"notify_target_agent_id"`
}

func (r inboxRespondRequest) core() inboxRespondRequestCore {
	return inboxRespondRequestCore{
		IdempotencyKey:      r.IdempotencyKey,
		ActorID:             r.ActorID,
		InboxItemID:         r.InboxItemID,
		ResponseText:        r.ResponseText,
		Outcome:             r.Outcome,
		RelatedRefs:         r.RelatedRefs,
		NotifyMode:          r.NotifyMode,
		NotifyTargetActorID: r.NotifyTargetActorID,
		NotifyTargetAgentID: r.NotifyTargetAgentID,
	}
}

func handleRespondInboxItem(w http.ResponseWriter, r *http.Request, opts handlerOptions, pathInboxItemID string) {
	if _, ok := requireHumanPrincipal(w, r, opts); !ok {
		return
	}
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}
	responseStore, ok := opts.primitiveStore.(humanAttentionResponseStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "human attention response store is not configured")
		return
	}

	var req inboxRespondRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	pathInboxItemID = strings.TrimSpace(pathInboxItemID)
	bodyItemID := strings.TrimSpace(req.InboxItemID)
	effectiveItemID := bodyItemID
	if pathInboxItemID != "" {
		if bodyItemID != "" && bodyItemID != pathInboxItemID {
			writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id must match path inbox_id")
			return
		}
		effectiveItemID = pathInboxItemID
	}
	if effectiveItemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id is required (body or path)")
		return
	}

	// Decode and validate the body before reserving answer priority. A stalled
	// upload must never hold the finite-read lane away from other readers.
	if opts.readAdmission != nil {
		release := opts.readAdmission.answer()
		defer release()
	}
	// A batch item answers every member in one atomic pass. Intercept before
	// the single-ask validations, which require flat response fields.
	if respondInboxBatchIfNeeded(w, r, opts, effectiveItemID, actorID, req) {
		return
	}

	responseText := strings.TrimSpace(req.ResponseText)
	if err := schema.ValidateEnum(opts.contract, "human_attention_response_outcome", req.Outcome); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if responseText == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "response_text is required")
		return
	}
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeField(w, "response_text", &responseText) {
		return
	}
	responseText = strings.TrimSpace(responseText)
	req.ResponseText = responseText
	if len(req.IdempotencyKey) > 128 || strings.TrimSpace(req.IdempotencyKey) != req.IdempotencyKey {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotency_key must be at most 128 characters without surrounding whitespace")
		return
	}
	hashInput, err := json.Marshal(struct {
		ActorID     string `json:"actor_id"`
		InboxItemID string `json:"inbox_item_id"`
		Request     any    `json:"request"`
	}{actorID, effectiveItemID, req.core()})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to hash human attention response")
		return
	}
	digest := sha256.Sum256(hashInput)
	requestHash := hex.EncodeToString(digest[:])
	if !authorizeInboxResponseAccess(w, r, opts, responseStore, effectiveItemID) {
		return
	}
	if req.IdempotencyKey != "" {
		replay, replayErr := responseStore.HumanAttentionResponseReplay(r.Context(), actorID, req.IdempotencyKey, requestHash)
		if replayErr == nil {
			writeAuthorizedInboxResponseReplay(w, r, opts, replay)
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
	for _, variant := range inboxItemIDVariants(effectiveItemID) {
		claimed, claimErr := responseStore.HumanAttentionResponseClaimed(r.Context(), variant)
		if claimErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to check response state")
			return
		}
		if claimed {
			if req.IdempotencyKey != "" {
				replay, replayErr := responseStore.HumanAttentionResponseReplay(r.Context(), actorID, req.IdempotencyKey, requestHash)
				if replayErr == nil {
					writeAuthorizedInboxResponseReplay(w, r, opts, replay)
					return
				}
				if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
					writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
					return
				}
			}
			writeError(w, http.StatusConflict, "conflict", "Ask is no longer open")
			return
		}
	}

	item, err := resolveInboxItemByVariants(r.Context(), opts.primitiveStore, effectiveItemID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			if req.IdempotencyKey != "" {
				replay, replayErr := responseStore.HumanAttentionResponseReplay(r.Context(), actorID, req.IdempotencyKey, requestHash)
				if replayErr == nil {
					writeAuthorizedInboxResponseReplay(w, r, opts, replay)
					return
				}
				if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
					writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
					return
				}
			}
			for _, variant := range inboxItemIDVariants(effectiveItemID) {
				claimed, claimErr := responseStore.HumanAttentionResponseClaimed(r.Context(), variant)
				if claimErr == nil && claimed {
					writeError(w, http.StatusConflict, "conflict", "Ask is no longer open")
					return
				}
			}
			writeError(w, http.StatusNotFound, "not_found", "Inbox item not found; your response can be retried")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox item")
		return
	}

	itemPayload := cloneWorkspaceMap(item.Data)
	applyInboxContractShape(itemPayload, inboxContractHintFromDerived(item))
	if !inboxItemAccessible(r, opts, item.ThreadID, itemPayload) {
		denyPMNotFound(w, "inbox item")
		return
	}
	kind := canonicalHumanAttentionKind(anyString(itemPayload["kind"]))
	if kind == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox item is not a human attention item")
		return
	}

	threadID := strings.TrimSpace(item.ThreadID)
	if threadID == "" {
		threadID = strings.TrimSpace(anyString(itemPayload["thread_id"]))
	}
	if threadID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ask inbox item is missing backing thread_id")
		return
	}

	inboxItemID := strings.TrimSpace(anyString(itemPayload["id"]))
	if inboxItemID == "" {
		inboxItemID = strings.TrimSpace(item.ID)
	}
	if inboxItemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ask inbox item is missing id")
		return
	}

	subjectRef := strings.TrimSpace(anyString(itemPayload["subject_ref"]))
	relatedRefs, _ := extractStringSlice(itemPayload["related_refs"])
	relatedRefs = append(relatedRefs, normalizeStringSlice(req.RelatedRefs)...)
	// Added response evidence will also be disclosed by an idempotent replay.
	// Check the same merged refs before the initial response can commit.
	responseAccess := cloneWorkspaceMap(itemPayload)
	responseAccess["related_refs"] = relatedRefs
	if !inboxItemAccessible(r, opts, threadID, responseAccess) {
		denyPMNotFound(w, "inbox item")
		return
	}
	requesterActorID := strings.TrimSpace(anyString(itemPayload["requester_actor_id"]))
	requesterAgentID := strings.TrimSpace(anyString(itemPayload["requester_agent_id"]))
	requesterLabel := strings.TrimSpace(anyString(itemPayload["requester_label"]))
	sourceEventID := strings.TrimSpace(anyString(itemPayload["source_event_id"]))
	requestEventRef := strings.TrimSpace(anyString(itemPayload["request_event_ref"]))
	if requestEventRef == "" && sourceEventID != "" {
		requestEventRef = "event:" + sourceEventID
	}
	if sourceEventID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox item is missing source event id")
		return
	}

	// delivery=none on the ask itself opts the requesting agent out of wake
	// delivery server-side. The answer stays recorded and visible; no wake is
	// resolved, queued, or flushed for it, even when the human passes an
	// explicit notify target.
	askDelivery := strings.TrimSpace(anyString(itemPayload["delivery"]))
	if askDelivery == "" {
		askDelivery = askDeliveryModeFromSourceEvent(r.Context(), opts, strings.TrimSpace(anyString(itemPayload["source_event_id"])))
	}
	var target humanAttentionResponseTarget
	if askDelivery == "none" {
		target = humanAttentionResponseTarget{Mode: "none"}
	} else {
		var ok bool
		target, ok = resolveHumanAttentionResponseTarget(w, r, opts, humanAttentionTargetRequest{
			NotifyMode:          req.NotifyMode,
			NotifyTargetActorID: req.NotifyTargetActorID,
			NotifyTargetAgentID: req.NotifyTargetAgentID,
			RequesterActorID:    requesterActorID,
			RequesterAgentID:    requesterAgentID,
		})
		if !ok {
			return
		}
	}

	responseRefs := make([]string, 0, len(relatedRefs)+6)
	responseRefs = append(responseRefs, "thread:"+threadID, "inbox:"+inboxItemID)
	responseRefs = append(responseRefs, relatedRefs...)
	if subjectRef != "" {
		responseRefs = append(responseRefs, subjectRef)
	}
	if sourceEventID != "" {
		responseRefs = append(responseRefs, "event:"+sourceEventID)
	}
	if requestEventRef != "" {
		responseRefs = append(responseRefs, requestEventRef)
	}
	responseRefs = mergeUniqueSortedRefs(responseRefs...)

	summary := buildHumanAttentionResponseSummary(kind, itemPayload, responseText)
	responseEvent := map[string]any{
		"type":      humanAttentionRespondedEventType,
		"thread_id": threadID,
		"refs":      responseRefs,
		"summary":   summary,
		"payload": map[string]any{
			"inbox_item_id":       inboxItemID,
			"request_event_ref":   requestEventRef,
			"kind":                kind,
			"response_text":       responseText,
			"outcome":             req.Outcome,
			"subject_ref":         subjectRef,
			"related_refs":        responseRefs,
			"requester_actor_id":  requesterActorID,
			"requester_agent_id":  requesterAgentID,
			"requester_label":     requesterLabel,
			"responding_actor_id": actorID,
			"notified_actor_id":   target.ActorID,
			"notified_agent_id":   target.AgentID,
		},
		"provenance": eventProvenance(),
	}
	if err := validateEventReferenceConventions(opts.contract, responseEvent, responseRefs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	notifyRequested := target.Mode != "none"
	notifyMessageText := map[bool]string{true: "Answer wake queued for the requester.", false: "Response recorded without notification target."}[notifyRequested]
	if askDelivery == "none" {
		notifyMessageText = "Ask requested no delivery (mode none); response recorded without waking the requester."
	}
	initialNotify := map[string]any{
		"requested":       notifyRequested,
		"queued":          notifyRequested,
		"message":         notifyMessageText,
		"target_actor_id": target.ActorID,
		"target_agent_id": target.AgentID,
		"target_handle":   target.Handle,
		"workspace_id":    opts.workspaceID,
		"thread_id":       threadID,
		"subject_ref":     subjectRef,
		"mode":            target.Mode,
		"quiet_window_ns": int64(opts.answerWakeQuietWindow),
	}
	if accessStore, ok := opts.primitiveStore.(*primitives.Store); ok && notifyRequested {
		if request, err := accessStore.AccessRequestForEvent(r.Context(), sourceEventID); err == nil {
			initialNotify["related_refs"] = []string{"access-request:" + request.ID}
		}
	}
	// Persist the exact HTTP replay together with the answer. Projection refresh
	// and wake delivery are driven by durable queues, never the request lifetime.
	responseFields := hygiene.attach(map[string]any{"notify": map[string]any{
		"requested": notifyRequested, "queued": notifyRequested,
		"message": initialNotify["message"], "target_actor_id": target.ActorID,
		"target_agent_id": target.AgentID, "target_handle": target.Handle, "mode": target.Mode,
	}})
	storedResponse, replayed, err := responseStore.AppendHumanAttentionResponse(r.Context(), actorID, sourceEventID, inboxItemID, req.IdempotencyKey, requestHash, responseEvent, initialNotify, responseFields)
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
			writeError(w, http.StatusConflict, "conflict", "Ask is no longer open")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to store human attention response")
		return
	}
	if replayed {
		writeAuthorizedInboxResponseReplay(w, r, opts, storedResponse)
		return
	}
	notifyProjectionMaintainer(opts)
	// A fresh answer to a curation proposal applies or dismisses it. A replay
	// (including the early returns above) reapplies when the proposal was
	// accepted and the trail never landed.
	writeJSON(w, http.StatusCreated, storedResponse)
}
