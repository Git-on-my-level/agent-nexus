package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
	"agent-nexus-core/internal/schema"
)

type humanAttentionResponseStore interface {
	HumanAttentionResponseReplay(context.Context, string, string, string) (map[string]any, error)
	HumanAttentionResponseClaimed(context.Context, string) (bool, error)
	AppendHumanAttentionResponse(context.Context, string, string, string, string, string, map[string]any, map[string]any) (map[string]any, bool, error)
	SaveHumanAttentionResponseResult(context.Context, string, map[string]any) error
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

	var req struct {
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
	}{actorID, effectiveItemID, req})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to hash human attention response")
		return
	}
	digest := sha256.Sum256(hashInput)
	requestHash := hex.EncodeToString(digest[:])
	if req.IdempotencyKey != "" {
		replay, replayErr := responseStore.HumanAttentionResponseReplay(r.Context(), actorID, req.IdempotencyKey, requestHash)
		if replayErr == nil {
			writeJSON(w, http.StatusCreated, replay)
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
					writeJSON(w, http.StatusCreated, replay)
					return
				}
				if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
					writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
					return
				}
			}
			writeError(w, http.StatusConflict, "conflict", "human attention request already has a response")
			return
		}
	}

	item, err := resolveInboxItemByVariants(r.Context(), opts.primitiveStore, effectiveItemID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			if req.IdempotencyKey != "" {
				replay, replayErr := responseStore.HumanAttentionResponseReplay(r.Context(), actorID, req.IdempotencyKey, requestHash)
				if replayErr == nil {
					writeJSON(w, http.StatusCreated, replay)
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
					writeError(w, http.StatusConflict, "conflict", "human attention request already has a response")
					return
				}
			}
			writeError(w, http.StatusNotFound, "not_found", "inbox item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox item")
		return
	}

	itemPayload := cloneWorkspaceMap(item.Data)
	applyInboxContractShape(itemPayload, inboxContractHintFromDerived(item))
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

	target, ok := resolveHumanAttentionResponseTarget(w, r, opts, humanAttentionTargetRequest{
		NotifyMode:          req.NotifyMode,
		NotifyTargetActorID: req.NotifyTargetActorID,
		NotifyTargetAgentID: req.NotifyTargetAgentID,
		RequesterActorID:    requesterActorID,
		RequesterAgentID:    requesterAgentID,
	})
	if !ok {
		return
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
	initialNotify := map[string]any{
		"requested":       notifyRequested,
		"queued":          notifyRequested,
		"message":         map[bool]string{true: "Answer wake queued for the requester.", false: "Response recorded without notification target."}[notifyRequested],
		"target_actor_id": target.ActorID,
		"target_agent_id": target.AgentID,
		"target_handle":   target.Handle,
		"workspace_id":    opts.workspaceID,
		"thread_id":       threadID,
		"subject_ref":     subjectRef,
		"mode":            target.Mode,
		"quiet_window_ns": int64(opts.answerWakeQuietWindow),
	}
	storedResponse, replayed, err := responseStore.AppendHumanAttentionResponse(r.Context(), actorID, sourceEventID, inboxItemID, req.IdempotencyKey, requestHash, responseEvent, initialNotify)
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidAccessDecision) || errors.Is(err, auth.ErrInvalidRequest) || errors.Is(err, auth.ErrAgentNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, auth.ErrHumanRequired) {
			writeError(w, http.StatusForbidden, "human_required", "only active humans can decide access requests")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) {
			writeError(w, http.StatusConflict, "conflict", "human attention request already has a response")
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
		writeJSON(w, http.StatusCreated, storedResponse)
		return
	}
	responseStored := storedResponse["event"].(map[string]any)
	_ = refreshDerivedTopicProjection(r.Context(), opts, threadID, time.Now().UTC(), actorID)

	notifyQueued := notifyRequested
	notifyMessage := anyString(initialNotify["message"])
	if notifyRequested && opts.answerWakeFlushWhenNoOpenAsks {
		if batchStore, ok := opts.primitiveStore.(interface {
			CountOpenHumanAttentionAsks(context.Context, string) (int, error)
		}); ok {
			if openAsks, countErr := batchStore.CountOpenHumanAttentionAsks(r.Context(), requesterActorID); countErr == nil && openAsks == 0 {
				maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
					PrimitiveStore:      opts.primitiveStore,
					WorkspaceID:         opts.workspaceID,
					QuietWindow:         opts.answerWakeQuietWindow,
					FlushWhenNoOpenAsks: true,
					RecipientIsActive:   answerWakeRecipientIsActive(opts.authStore),
				})
				if flushErr := maintainer.FlushTarget(r.Context(), target.ActorID); flushErr == nil {
					notifyMessage = "All of your open asks are answered; the answer batch is queued now."
				}
			}
		}
	}

	response := map[string]any{
		"event": responseStored,
		"notify": map[string]any{
			"requested":       notifyRequested,
			"queued":          notifyQueued,
			"message":         notifyMessage,
			"target_actor_id": target.ActorID,
			"target_agent_id": target.AgentID,
			"target_handle":   target.Handle,
			"mode":            target.Mode,
		},
	}

	response = hygiene.attach(response)
	if err := responseStore.SaveHumanAttentionResponseResult(r.Context(), sourceEventID, response); err != nil {
		log.Printf("save human attention response replay: %v", err)
	}
	writeJSON(w, http.StatusCreated, response)
}

func resolveInboxItemByVariants(ctx context.Context, store PrimitiveStore, inboxItemID string) (primitives.DerivedInboxItem, error) {
	for _, candidate := range inboxItemIDVariants(strings.TrimSpace(inboxItemID)) {
		item, err := store.GetDerivedInboxItem(ctx, candidate)
		if err == nil {
			return item, nil
		}
		if !errors.Is(err, primitives.ErrNotFound) {
			return primitives.DerivedInboxItem{}, err
		}
	}
	return primitives.DerivedInboxItem{}, primitives.ErrNotFound
}

type humanAttentionTargetRequest struct {
	NotifyMode          string
	NotifyTargetActorID string
	NotifyTargetAgentID string
	RequesterActorID    string
	RequesterAgentID    string
}

type humanAttentionResponseTarget struct {
	Mode    string
	ActorID string
	AgentID string
	Handle  string
}

func resolveHumanAttentionResponseTarget(
	w http.ResponseWriter,
	r *http.Request,
	opts handlerOptions,
	req humanAttentionTargetRequest,
) (humanAttentionResponseTarget, bool) {
	mode := strings.ToLower(strings.TrimSpace(req.NotifyMode))
	if mode == "" {
		mode = "original"
	}
	if strings.TrimSpace(req.NotifyTargetActorID) != "" || strings.TrimSpace(req.NotifyTargetAgentID) != "" {
		mode = "target"
	}

	switch mode {
	case "none":
		return humanAttentionResponseTarget{Mode: "none"}, true
	case "original":
		target, found, err := resolveAgentNotificationTarget(r.Context(), opts, req.RequesterActorID, req.RequesterAgentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve requester notification target")
			return humanAttentionResponseTarget{}, false
		}
		if !found {
			writeError(w, http.StatusConflict, "notification_target_required", "original requester is not resolvable; choose a replacement notification target or submit notify_mode=none")
			return humanAttentionResponseTarget{}, false
		}
		target.Mode = "original"
		return target, true
	case "target":
		target, found, err := resolveAgentNotificationTarget(r.Context(), opts, req.NotifyTargetActorID, req.NotifyTargetAgentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve notification target")
			return humanAttentionResponseTarget{}, false
		}
		if !found {
			writeError(w, http.StatusConflict, "notification_target_required", "notification target is not resolvable; choose another target or submit notify_mode=none")
			return humanAttentionResponseTarget{}, false
		}
		target.Mode = "target"
		return target, true
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "notify_mode must be original, target, or none")
		return humanAttentionResponseTarget{}, false
	}
}

func resolveAgentNotificationTarget(ctx context.Context, opts handlerOptions, actorID string, agentID string) (humanAttentionResponseTarget, bool, error) {
	if opts.authStore == nil {
		return humanAttentionResponseTarget{}, false, nil
	}
	actorID = strings.TrimSpace(actorID)
	agentID = strings.TrimSpace(agentID)
	var principal auth.AuthPrincipalSummary
	var found bool
	var err error
	if agentID != "" {
		principal, found, err = findAgentPrincipalByAgentID(ctx, opts.authStore, agentID)
	} else if actorID != "" {
		principal, found, err = findAgentPrincipalByActorID(ctx, opts.authStore, actorID)
	}
	if err != nil || !found {
		return humanAttentionResponseTarget{}, found, err
	}
	return humanAttentionResponseTarget{
		ActorID: principal.ActorID,
		AgentID: principal.AgentID,
		Handle:  principal.Username,
	}, true, nil
}

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func resolveOptionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func buildAskResponseSummary(queryText, answer string) string {
	queryText = strings.TrimSpace(queryText)
	answer = strings.TrimSpace(answer)
	if queryText == "" {
		return "Answered agent ask"
	}
	if len(queryText) > 72 {
		queryText = strings.TrimSpace(queryText[:72]) + "…"
	}
	if answer == "" {
		return "Answered: " + queryText
	}
	return "Answered: " + queryText
}

func buildHumanAttentionResponseSummary(kind string, itemPayload map[string]any, responseText string) string {
	title := strings.TrimSpace(anyString(itemPayload["title"]))
	if title == "" {
		title = strings.TrimSpace(anyString(itemPayload["body"]))
	}
	if title == "" {
		title = strings.TrimSpace(kind)
	}
	if len(title) > 72 {
		title = strings.TrimSpace(title[:72]) + "..."
	}
	return "Human response recorded: " + title
}

func sendHumanAttentionResponseWakeBestEffort(
	ctx context.Context,
	opts handlerOptions,
	actorID string,
	threadID string,
	subjectRef string,
	targetActorID string,
	targetHandle string,
	triggerText string,
	triggerEventID string,
	triggerCreatedAt string,
) (bool, string) {
	targetActorID = strings.TrimSpace(targetActorID)
	if targetActorID == "" {
		return false, "Response recorded without notification target."
	}
	workspaceID := strings.TrimSpace(opts.workspaceID)
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	targetHandle = strings.TrimSpace(targetHandle)
	if targetHandle == "" {
		targetHandle = "agent"
	}
	online := false
	if opts.authStore != nil {
		principal, found, err := findAgentPrincipalByActorID(ctx, opts.authStore, targetActorID)
		if err == nil && found {
			if strings.TrimSpace(principal.Username) != "" {
				targetHandle = strings.TrimSpace(principal.Username)
			}
			status := auth.DescribeWakeRouting(principal, workspaceID, time.Now().UTC())
			online = status.Online
		}
	}

	if triggerEventID == "" {
		triggerEventID = fmt.Sprintf("ask-response:%d", time.Now().UTC().UnixNano())
	}
	if triggerCreatedAt == "" {
		triggerCreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	wakeupID := router.WakeupArtifactID(workspaceID, threadID, triggerEventID, targetActorID)
	wakeRefs := append(router.WakeArtifactRefs(threadID, triggerEventID, subjectRef), "artifact:"+wakeupID)

	_, artifactErr := opts.primitiveStore.CreateArtifact(ctx, actorID, map[string]any{
		"id":              wakeupID,
		"kind":            router.WakeArtifactKind,
		"summary":         "Wake packet for @" + targetHandle,
		"refs":            router.WakeArtifactRefs(threadID, triggerEventID, subjectRef),
		"target_handle":   targetHandle,
		"target_actor_id": targetActorID,
		"workspace_id":    workspaceID,
		"thread_id":       threadID,
	}, map[string]any{
		"version":            router.WakePacketVersion,
		"wakeup_id":          wakeupID,
		"target_handle":      targetHandle,
		"target_actor_id":    targetActorID,
		"workspace_id":       workspaceID,
		"thread_id":          threadID,
		"trigger_event_id":   triggerEventID,
		"trigger_created_at": triggerCreatedAt,
		"trigger_text":       triggerText,
		"subject_ref":        subjectRef,
	}, "structured")
	if artifactErr != nil && !errors.Is(artifactErr, primitives.ErrConflict) {
		// Continue: queue metadata still carries enough wake metadata for delivery.
	}

	_, wakeErr := opts.primitiveStore.UpsertAgentWakeup(ctx, primitives.AgentWakeup{
		WakeupID:         wakeupID,
		Status:           primitives.AgentWakeupStatusRequested,
		TargetHandle:     targetHandle,
		TargetActorID:    targetActorID,
		WorkspaceID:      workspaceID,
		ThreadID:         threadID,
		TriggerEventID:   triggerEventID,
		TriggerCreatedAt: triggerCreatedAt,
		TriggerText:      triggerText,
		Refs:             wakeRefs,
	})
	if wakeErr != nil && !errors.Is(wakeErr, primitives.ErrConflict) {
		return true, "Queued — will deliver when agent reconnects."
	}

	if online {
		return false, "Delivered to asking agent."
	}
	return true, "Queued — will deliver when agent reconnects."
}

func findAgentPrincipalByActorID(ctx context.Context, authStore *auth.Store, actorID string) (auth.AuthPrincipalSummary, bool, error) {
	if authStore == nil {
		return auth.AuthPrincipalSummary{}, false, nil
	}
	principals, _, err := authStore.ListPrincipals(ctx, auth.AuthPrincipalListFilter{})
	if err != nil {
		return auth.AuthPrincipalSummary{}, false, err
	}
	wantedActorID := strings.TrimSpace(actorID)
	for _, principal := range principals {
		if strings.TrimSpace(principal.ActorID) != wantedActorID {
			continue
		}
		if principal.Revoked || strings.TrimSpace(principal.PrincipalKind) != "agent" {
			continue
		}
		return principal, true, nil
	}
	return auth.AuthPrincipalSummary{}, false, nil
}

func findAgentPrincipalByAgentID(ctx context.Context, authStore *auth.Store, agentID string) (auth.AuthPrincipalSummary, bool, error) {
	if authStore == nil {
		return auth.AuthPrincipalSummary{}, false, nil
	}
	principals, _, err := authStore.ListPrincipals(ctx, auth.AuthPrincipalListFilter{})
	if err != nil {
		return auth.AuthPrincipalSummary{}, false, err
	}
	wantedAgentID := strings.TrimSpace(agentID)
	for _, principal := range principals {
		if strings.TrimSpace(principal.AgentID) != wantedAgentID {
			continue
		}
		if principal.Revoked || strings.TrimSpace(principal.PrincipalKind) != "agent" {
			continue
		}
		return principal, true, nil
	}
	return auth.AuthPrincipalSummary{}, false, nil
}
