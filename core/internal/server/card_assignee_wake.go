package server

import (
	"context"
	"errors"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
	"agent-nexus-core/internal/schema"
)

// Legacy unkeyed handlers retain best-effort assignment wake scheduling.
func enqueueCardAssigneeWakeBestEffort(
	ctx context.Context,
	opts handlerOptions,
	actingActorID string,
	prevCard map[string]any,
	newCard map[string]any,
	board map[string]any,
	storedLifecycleEvent map[string]any,
) {
	if prevCard != nil && canonicalAssigneeStorageString(prevCard) == canonicalAssigneeStorageString(newCard) {
		return
	}
	target, err := prepareCardAssigneeWakeTarget(ctx, opts, newCard)
	if err != nil {
		return
	}
	_ = storeCardAssigneeWake(ctx, opts, actingActorID, prevCard, newCard, board, storedLifecycleEvent, target, false)
}

type cardAssigneeWakeTarget struct{ actorID, handle string }

// Authentication reads use their own store. Resolve the bounded indexed target
// before taking the primitive transaction; canonical refs are rechecked on writes.
func prepareCardAssigneeWakeTarget(ctx context.Context, opts handlerOptions, newCard map[string]any) (*cardAssigneeWakeTarget, error) {
	refs := cardAssigneeRefs(newCard)
	if len(refs) == 0 {
		return nil, nil
	}
	ref := strings.TrimSpace(refs[0])
	if ref == "" {
		return nil, nil
	}
	prefix, id, err := schema.SplitTypedRef(ref)
	if err != nil {
		return nil, nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}

	var targetActorID string
	var targetHandle string
	switch prefix {
	case "agent":
		if opts.authStore == nil {
			return nil, nil
		}
		p, found, err := findAgentPrincipalByAgentID(ctx, opts.authStore, id)
		if err != nil || !found {
			return nil, err
		}
		targetActorID = p.ActorID
		targetHandle = strings.TrimSpace(p.Username)
	case "actor":
		if opts.authStore == nil {
			return nil, nil
		}
		p, found, err := findAgentPrincipalByActorID(ctx, opts.authStore, id)
		if err != nil || !found {
			return nil, err
		}
		targetActorID = p.ActorID
		targetHandle = strings.TrimSpace(p.Username)
	case "human":
		return nil, nil
	default:
		return nil, nil
	}

	return &cardAssigneeWakeTarget{targetActorID, targetHandle}, nil
}

func storeCardAssigneeWake(ctx context.Context, opts handlerOptions, actingActorID string, prevCard, newCard, board, storedLifecycleEvent map[string]any, target *cardAssigneeWakeTarget, strict bool) error {
	if target == nil {
		return nil
	}
	if opts.primitiveStore == nil || newCard == nil || storedLifecycleEvent == nil {
		if strict {
			return errors.New("assignment wake context incomplete")
		}
		return nil
	}

	prevCanonical := ""
	if prevCard != nil {
		prevCanonical = canonicalAssigneeStorageString(prevCard)
	}
	nextCanonical := canonicalAssigneeStorageString(newCard)
	if nextCanonical == "" || nextCanonical == prevCanonical {
		return nil
	}

	targetActorID, targetHandle := target.actorID, target.handle
	threadID := strings.TrimSpace(anyString(newCard["thread_id"]))
	if threadID == "" {
		if strict {
			return errors.New("assignment wake thread missing")
		}
		return nil
	}

	triggerEventID := strings.TrimSpace(anyString(storedLifecycleEvent["id"]))
	triggerCreatedAt := strings.TrimSpace(anyString(storedLifecycleEvent["ts"]))
	if triggerEventID == "" {
		if strict {
			return errors.New("assignment wake event missing")
		}
		return nil
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
	if strict && artifactErr != nil {
		return artifactErr
	}
	if artifactErr != nil && !errors.Is(artifactErr, primitives.ErrConflict) {
		// Best-effort: wakeup queue metadata remains usable without artifact.
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
	if strict {
		return wakeErr
	}
	return nil
}
