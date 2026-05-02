package server

import (
	"context"
	"errors"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
	"agent-nexus-core/internal/schema"
)

// enqueueCardAssigneeWakeBestEffort queues an agent wakeup when a card assignee changes to a
// registered agent, keyed to the emitted card_created/card_updated lifecycle event.
func enqueueCardAssigneeWakeBestEffort(
	ctx context.Context,
	opts handlerOptions,
	actingActorID string,
	prevCard map[string]any,
	newCard map[string]any,
	board map[string]any,
	storedLifecycleEvent map[string]any,
) {
	if opts.primitiveStore == nil || newCard == nil || storedLifecycleEvent == nil {
		return
	}

	prevCanonical := ""
	if prevCard != nil {
		prevCanonical = canonicalAssigneeStorageString(prevCard)
	}
	nextCanonical := canonicalAssigneeStorageString(newCard)
	if nextCanonical == "" || nextCanonical == prevCanonical {
		return
	}

	refs := cardAssigneeRefs(newCard)
	if len(refs) == 0 {
		return
	}
	ref := strings.TrimSpace(refs[0])
	if ref == "" {
		return
	}
	prefix, id, err := schema.SplitTypedRef(ref)
	if err != nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}

	var targetActorID string
	var targetHandle string
	switch prefix {
	case "agent":
		if opts.authStore == nil {
			return
		}
		p, found, err := findAgentPrincipalByAgentID(ctx, opts.authStore, id)
		if err != nil || !found {
			return
		}
		targetActorID = p.ActorID
		targetHandle = strings.TrimSpace(p.Username)
	case "actor":
		if opts.authStore == nil {
			return
		}
		p, found, err := findAgentPrincipalByActorID(ctx, opts.authStore, id)
		if err != nil || !found {
			return
		}
		targetActorID = p.ActorID
		targetHandle = strings.TrimSpace(p.Username)
	case "human":
		return
	default:
		return
	}

	threadID := strings.TrimSpace(anyString(newCard["thread_id"]))
	if threadID == "" {
		return
	}

	triggerEventID := strings.TrimSpace(anyString(storedLifecycleEvent["id"]))
	triggerCreatedAt := strings.TrimSpace(anyString(storedLifecycleEvent["ts"]))
	if triggerEventID == "" {
		return
	}

	workspaceID := strings.TrimSpace(opts.workspaceID)
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	if targetHandle == "" {
		targetHandle = "agent"
	}

	cardID := strings.TrimSpace(anyString(newCard["id"]))
	cardTitle := strings.TrimSpace(anyString(newCard["title"]))
	triggerText := "You were assigned card"
	if cardTitle != "" {
		triggerText += ": " + cardTitle
	}
	if board != nil {
		bid := strings.TrimSpace(anyString(board["id"]))
		if bid != "" {
			triggerText += " (board:" + bid + ")"
		}
	}

	subjectRef := ""
	if cardID != "" {
		subjectRef = "card:" + cardID
	}

	wakeupID := router.WakeupArtifactID(workspaceID, threadID, triggerEventID, targetActorID)
	wakeRefs := append(router.WakeArtifactRefs(threadID, triggerEventID, subjectRef), "artifact:"+wakeupID)

	_, artifactErr := opts.primitiveStore.CreateArtifact(ctx, actingActorID, map[string]any{
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
		// Best-effort: wakeup queue metadata remains usable without artifact.
	}

	_, _ = opts.primitiveStore.UpsertAgentWakeup(ctx, primitives.AgentWakeup{
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
}
