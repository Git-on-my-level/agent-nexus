package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
)

func handleRespondInboxItem(w http.ResponseWriter, r *http.Request, opts handlerOptions, pathInboxItemID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID             string   `json:"actor_id"`
		InboxItemID         string   `json:"inbox_item_id"`
		ResponseText        string   `json:"response_text"`
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
	if responseText == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "response_text is required")
		return
	}

	item, err := resolveInboxItemByVariants(r.Context(), opts.primitiveStore, effectiveItemID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
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

	responseStored, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, responseEvent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to store human attention response")
		return
	}
	_ = refreshDerivedTopicProjection(r.Context(), opts, threadID, time.Now().UTC(), actorID)

	notifyRequested := target.Mode != "none"
	notifyQueued := false
	notifyMessage := ""
	if notifyRequested {
		notifyQueued, notifyMessage = sendHumanAttentionResponseWakeBestEffort(
			r.Context(),
			opts,
			actorID,
			threadID,
			subjectRef,
			target.ActorID,
			target.Handle,
			summary,
			strings.TrimSpace(anyString(responseStored["id"])),
			strings.TrimSpace(anyString(responseStored["ts"])),
		)
	} else {
		notifyMessage = "Response recorded without notification target."
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
	sessionKey := fmt.Sprintf("anx:%s:%s:%s", workspaceID, threadID, targetHandle)

	wakePayload := router.BuildWakeRequestPayload(
		wakeupID,
		targetHandle,
		targetActorID,
		workspaceID,
		"Main",
		threadID,
		triggerEventID,
		triggerCreatedAt,
		triggerText,
		sessionKey,
		subjectRef,
		nil,
	)

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
		// Continue: event payload still carries enough wake metadata for queue delivery.
	}

	wakeEvent := map[string]any{
		"type":      router.WakeRequestEvent,
		"thread_id": threadID,
		"summary":   "Wake requested for @" + targetHandle,
		"refs":      wakeRefs,
		"payload":   wakePayload,
		"provenance": map[string]any{
			"sources": []string{"event:" + triggerEventID},
		},
	}
	if err := validateEventReferenceConventions(opts.contract, wakeEvent, wakeRefs); err == nil {
		_, appendErr := opts.primitiveStore.AppendEvent(ctx, actorID, wakeEvent)
		if appendErr != nil && !errors.Is(appendErr, primitives.ErrConflict) {
			return true, "Queued — will deliver when agent reconnects."
		}
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
