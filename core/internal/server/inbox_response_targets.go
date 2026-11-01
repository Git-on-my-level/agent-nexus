// Shared plumbing around inbox responses: response authorization, replay
// writes for already-answered items, resolving which ask or agent notification
// a response targets, response summaries, the best-effort agent wake, and the
// delivery-mode lookup for asks whose projection predates delivery fields.

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

// Authorization precedes replay and claim lookups so an inaccessible request
// also looks absent when it has already been answered or the key conflicts.
func authorizeInboxResponseAccess(w http.ResponseWriter, r *http.Request, opts handlerOptions, store humanAttentionResponseStore, id string) bool {
	item, err := resolveInboxItemByVariants(r.Context(), opts.primitiveStore, id)
	if err == nil {
		if inboxItemAccessible(r, opts, item.ThreadID, payloadFromDerivedInboxItem(item)) {
			return true
		}
	} else if errors.Is(err, primitives.ErrNotFound) {
		request, requestErr := store.HumanAttentionResponseRequest(r.Context(), id)
		if requestErr == nil {
			requestPayload, ok := request["payload"].(map[string]any)
			if !ok {
				denyPMNotFound(w, "inbox item")
				return false
			}
			payload := cloneWorkspaceMap(requestPayload)
			payload["related_refs"] = append(stringSliceAny(payload["related_refs"]), stringSliceAny(request["refs"])...)
			if inboxItemAccessible(r, opts, eventThreadID(request), payload) {
				return true
			}
		} else if !errors.Is(requestErr, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to authorize inbox item")
			return false
		}
	} else {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to authorize inbox item")
		return false
	}
	denyPMNotFound(w, "inbox item")
	return false
}

// A successful response removes the open projection. Replays therefore use
// the persisted response's original subject/refs, with current authorization,
// rather than trusting either the replay key or a now-missing open Inbox row.
func writeAuthorizedInboxResponseReplay(w http.ResponseWriter, r *http.Request, opts handlerOptions, replay map[string]any) {
	event, _ := replay["event"].(map[string]any)
	payload, _ := event["payload"].(map[string]any)
	if event == nil || payload == nil || !inboxItemAccessible(r, opts, eventThreadID(event), payload) {
		denyPMNotFound(w, "inbox item")
		return
	}
	// The answer is already durable. Finish a curation action that did not
	// land with it, then return the original response.
	writeJSON(w, http.StatusCreated, replay)
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
	// Decide is backed by canonical ask facts, not the asynchronous inbox
	// projection. Recover a single ask from its immutable identity when that
	// cache is absent. Never infer identity from presentation/audit events.
	for _, candidate := range inboxItemIDVariants(strings.TrimSpace(inboxItemID)) {
		if !strings.HasPrefix(candidate, "inbox:") {
			continue
		}
		sourceID := candidate[strings.LastIndex(candidate, ":")+1:]
		if sourceID == "" {
			continue
		}
		event, err := store.GetEvent(ctx, sourceID)
		if errors.Is(err, primitives.ErrNotFound) {
			continue
		}
		if err != nil {
			return primitives.DerivedInboxItem{}, err
		}
		payload, _ := event["payload"].(map[string]any)
		// Filed batches must still use their atomic batch admission path.
		if event["type"] != humanAttentionRequestedEventType || anyString(event["trashed_at"]) != "" || anyString(event["archived_at"]) != "" || anyString(payload["batch_id"]) != "" {
			continue
		}
		item, ok := deriveHumanAttentionInboxItem(event)
		if !ok || item.ID != candidate {
			continue // Validate every segment; a guessed suffix is not an alias.
		}
		if _, err := store.GetThread(ctx, anyString(item.Data["thread_id"])); err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			return primitives.DerivedInboxItem{}, err
		}
		// Keep the same audience check even while no projection exists.
		if recipient := anyString(payload["recipient_actor_id"]); recipient != "" {
			item.Data["recipient_actor_id"] = recipient
		}
		return primitives.DerivedInboxItem{ID: item.ID, ThreadID: anyString(item.Data["thread_id"]), Category: item.Category, SourceEventID: sourceID, Data: item.Data}, nil
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
	targets, err := authStore.NotificationTargets(ctx, []string{strings.TrimSpace(actorID)}, nil)
	p, found := targets["actor:"+strings.TrimSpace(actorID)]
	return p, found, err
}

func findAgentPrincipalByAgentID(ctx context.Context, authStore *auth.Store, agentID string) (auth.AuthPrincipalSummary, bool, error) {
	if authStore == nil {
		return auth.AuthPrincipalSummary{}, false, nil
	}
	targets, err := authStore.NotificationTargets(ctx, nil, []string{strings.TrimSpace(agentID)})
	p, found := targets["agent:"+strings.TrimSpace(agentID)]
	return p, found, err
}

// askDeliveryModeFromSourceEvent reads the delivery mode recorded on the ask
// event itself. It covers items whose projection predates delivery fields.
func askDeliveryModeFromSourceEvent(ctx context.Context, opts handlerOptions, sourceEventID string) string {
	if opts.primitiveStore == nil || strings.TrimSpace(sourceEventID) == "" {
		return ""
	}
	event, err := opts.primitiveStore.GetEvent(ctx, strings.TrimSpace(sourceEventID))
	if err != nil {
		return ""
	}
	payload, _ := event["payload"].(map[string]any)
	return strings.TrimSpace(anyString(payload["delivery"]))
}
