package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/router"
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
		ActorID           string `json:"actor_id"`
		InboxItemID       string `json:"inbox_item_id"`
		Answer            string `json:"answer"`
		SaveAsDecision    *bool  `json:"save_as_decision"`
		NotifyAskingAgent *bool  `json:"notify_asking_agent"`
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

	answer := strings.TrimSpace(req.Answer)
	if answer == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "answer is required")
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
	kind := strings.ToLower(strings.TrimSpace(anyString(itemPayload["kind"])))
	if kind != "ask" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox item is not an ask item")
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
	queryText := strings.TrimSpace(anyString(itemPayload["query_text"]))
	askingAgentID := strings.TrimSpace(anyString(itemPayload["asking_agent_id"]))
	coverageHint := strings.TrimSpace(anyString(itemPayload["coverage_hint"]))
	sourceEventID := strings.TrimSpace(anyString(itemPayload["source_event_id"]))

	responseRefs := make([]string, 0, len(relatedRefs)+6)
	responseRefs = append(responseRefs, "thread:"+threadID, "inbox:"+inboxItemID)
	responseRefs = append(responseRefs, relatedRefs...)
	if subjectRef != "" {
		responseRefs = append(responseRefs, subjectRef)
	}
	if sourceEventID != "" {
		responseRefs = append(responseRefs, "event:"+sourceEventID)
	}
	responseRefs = mergeUniqueSortedRefs(responseRefs...)

	summary := buildAskResponseSummary(queryText, answer)
	responseEvent := map[string]any{
		"type":      agentAskAnsweredEventType,
		"thread_id": threadID,
		"refs":      responseRefs,
		"summary":   summary,
		"payload": map[string]any{
			"inbox_item_id":   inboxItemID,
			"query_text":      queryText,
			"answer":          answer,
			"asking_agent_id": askingAgentID,
			"coverage_hint":   coverageHint,
			"subject_ref":     subjectRef,
		},
		"provenance": actorStatementProvenance(),
	}
	if err := validateEventReferenceConventions(opts.contract, responseEvent, responseRefs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	responseStored, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, responseEvent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to store ask response")
		return
	}

	ackRefs := []string{"inbox:" + inboxItemID}
	ackEvent := map[string]any{
		"type":      "inbox_item_acknowledged",
		"thread_id": threadID,
		"refs":      ackRefs,
		"summary":   "Ask response captured",
		"payload": map[string]any{
			"inbox_item_id": inboxItemID,
			"subject_ref":   subjectRef,
		},
		"provenance": actorStatementProvenance(),
	}
	if err := validateEventReferenceConventions(opts.contract, ackEvent, ackRefs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ackStored, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, ackEvent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to acknowledge answered ask item")
		return
	}

	shouldSaveDecision := resolveOptionalBool(req.SaveAsDecision, hasTopicRef(subjectRef, relatedRefs))
	var decisionStored map[string]any
	if shouldSaveDecision {
		decisionEvent, ok := buildAskDecisionEvent(
			threadID,
			inboxItemID,
			queryText,
			answer,
			subjectRef,
			relatedRefs,
			strings.TrimSpace(anyString(responseStored["id"])),
		)
		if ok {
			decisionRefs, _ := extractStringSlice(decisionEvent["refs"])
			if err := validateEventReferenceConventions(opts.contract, decisionEvent, decisionRefs); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			decisionStored, err = opts.primitiveStore.AppendEvent(r.Context(), actorID, decisionEvent)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "failed to store decision from ask response")
				return
			}
		}
	}

	notifyRequested := resolveOptionalBool(req.NotifyAskingAgent, true)
	notifyQueued := false
	notifyMessage := ""
	if notifyRequested {
		notifyQueued, notifyMessage = sendAskResponseWakeBestEffort(
			r.Context(),
			opts,
			actorID,
			threadID,
			subjectRef,
			askingAgentID,
			summary,
			strings.TrimSpace(anyString(responseStored["id"])),
			strings.TrimSpace(anyString(responseStored["ts"])),
		)
	}

	response := map[string]any{
		"event":          responseStored,
		"acknowledgment": ackStored,
		"notify": map[string]any{
			"requested":       notifyRequested,
			"queued":          notifyQueued,
			"message":         notifyMessage,
			"asking_agent_id": askingAgentID,
		},
	}
	if decisionStored != nil {
		response["decision_event"] = decisionStored
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

func resolveOptionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func hasTopicRef(subjectRef string, relatedRefs []string) bool {
	if strings.HasPrefix(strings.TrimSpace(subjectRef), "topic:") {
		return true
	}
	for _, ref := range relatedRefs {
		if strings.HasPrefix(strings.TrimSpace(ref), "topic:") {
			return true
		}
	}
	return false
}

func firstTopicRef(subjectRef string, relatedRefs []string) string {
	subjectRef = strings.TrimSpace(subjectRef)
	if strings.HasPrefix(subjectRef, "topic:") {
		return subjectRef
	}
	for _, ref := range relatedRefs {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "topic:") {
			return ref
		}
	}
	return ""
}

func buildAskDecisionEvent(threadID, inboxItemID, queryText, answer, subjectRef string, relatedRefs []string, responseEventID string) (map[string]any, bool) {
	topicRef := firstTopicRef(subjectRef, relatedRefs)
	if topicRef == "" {
		return nil, false
	}
	decisionRefs := make([]string, 0, len(relatedRefs)+8)
	decisionRefs = append(decisionRefs, "thread:"+threadID, "inbox:"+inboxItemID, topicRef)
	decisionRefs = append(decisionRefs, relatedRefs...)
	if subjectRef != "" {
		decisionRefs = append(decisionRefs, subjectRef)
	}
	if responseEventID != "" {
		decisionRefs = append(decisionRefs, "event:"+responseEventID)
	}
	decisionRefs = mergeUniqueSortedRefs(decisionRefs...)
	return map[string]any{
		"type":      "decision_made",
		"thread_id": threadID,
		"refs":      decisionRefs,
		"summary":   buildAskDecisionSummary(queryText, answer),
		"payload": map[string]any{
			"inbox_item_id": inboxItemID,
			"notes":         answer,
			"source":        "ask_response",
		},
		"provenance": actorStatementProvenance(),
	}, true
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

func buildAskDecisionSummary(queryText, answer string) string {
	queryText = strings.TrimSpace(queryText)
	answer = strings.TrimSpace(answer)
	if queryText != "" {
		if len(queryText) > 72 {
			queryText = strings.TrimSpace(queryText[:72]) + "…"
		}
		return "Decision recorded from ask: " + queryText
	}
	if answer == "" {
		return "Decision recorded from ask response"
	}
	if len(answer) > 72 {
		answer = strings.TrimSpace(answer[:72]) + "…"
	}
	return "Decision recorded from ask response: " + answer
}

func sendAskResponseWakeBestEffort(
	ctx context.Context,
	opts handlerOptions,
	actorID string,
	threadID string,
	subjectRef string,
	askingAgentID string,
	triggerText string,
	triggerEventID string,
	triggerCreatedAt string,
) (bool, string) {
	if strings.TrimSpace(askingAgentID) == "" {
		return true, "Queued — will deliver when agent reconnects."
	}
	workspaceID := strings.TrimSpace(opts.workspaceID)
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	targetHandle := "agent"
	online := false
	if opts.authStore != nil {
		principal, found, err := findAgentPrincipalByActorID(ctx, opts.authStore, askingAgentID)
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

	wakeupID := router.WakeupArtifactID(workspaceID, threadID, triggerEventID, askingAgentID)
	wakeRefs := append(router.WakeArtifactRefs(threadID, triggerEventID, subjectRef), "artifact:"+wakeupID)
	sessionKey := fmt.Sprintf("oar:%s:%s:%s", workspaceID, threadID, targetHandle)

	wakePayload := router.BuildWakeRequestPayload(
		wakeupID,
		targetHandle,
		askingAgentID,
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
		"target_actor_id": askingAgentID,
		"workspace_id":    workspaceID,
		"thread_id":       threadID,
	}, map[string]any{
		"version":            router.WakePacketVersion,
		"wakeup_id":          wakeupID,
		"target_handle":      targetHandle,
		"target_actor_id":    askingAgentID,
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
			"sources": []string{"actor_statement:" + triggerEventID},
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
