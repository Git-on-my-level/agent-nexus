package server

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
)

type escalationWakeStore interface {
	CreateArtifact(context.Context, string, map[string]any, any, string) (map[string]any, error)
	UpsertAgentWakeup(context.Context, primitives.AgentWakeup) (primitives.AgentWakeup, error)
}

// NewEscalationPMWaker returns the escalation clock's AskPM hook: when an
// ask passes decide_by unanswered it wakes the workspace PM (through the
// ordinary durable wake path) to answer, withdraw or re-ask. The wake id is
// derived from the ask, so a repeated escalation is a no-op. It returns nil
// when no PM is configured.
func NewEscalationPMWaker(db *sql.DB, store PrimitiveStore, authStore *auth.Store, pmActorID func() string, workspaceID, workspaceName string) func(context.Context, string, time.Time) error {
	wakeStore, ok := store.(escalationWakeStore)
	if !ok || db == nil || authStore == nil || pmActorID == nil {
		return nil
	}
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = "ws_main"
	}
	return func(ctx context.Context, askID string, decideBy time.Time) error {
		actorID := strings.TrimSpace(pmActorID())
		if actorID == "" {
			return nil
		}
		principal, found, err := findAgentPrincipalByActorID(ctx, authStore, actorID)
		if err != nil || !found {
			return err
		}
		var threadID, ts string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(thread_id,''), ts FROM events WHERE id=?`, askID).Scan(&threadID, &ts); err != nil {
			return err
		}
		handle := strings.TrimSpace(principal.Username)
		if handle == "" {
			handle = "agent"
		}
		askRef := "event:" + askID
		wakeupID := router.WakeupArtifactID(workspaceID, threadID, "escalation-"+askID, actorID)
		text := "Ask " + askRef + " passed its decide_by (" + decideBy.UTC().Format(time.RFC3339) + ") unanswered. Answer it, withdraw it, or re-ask with a new --by."
		refs := router.WakeArtifactRefs(threadID, askID, askRef)
		_, err = wakeStore.CreateArtifact(ctx, actors.SystemActorID, map[string]any{
			"id":              wakeupID,
			"kind":            router.WakeArtifactKind,
			"summary":         "Overdue ask for @" + handle,
			"refs":            refs,
			"target_handle":   handle,
			"target_actor_id": actorID,
			"workspace_id":    workspaceID,
			"thread_id":       threadID,
		}, map[string]any{
			"version":            router.WakePacketVersion,
			"wakeup_id":          wakeupID,
			"target_handle":      handle,
			"target_actor_id":    actorID,
			"workspace_id":       workspaceID,
			"thread_id":          threadID,
			"trigger_event_id":   askID,
			"trigger_created_at": ts,
			"trigger_text":       text,
			"subject_ref":        askRef,
		}, "structured")
		if err != nil && !errors.Is(err, primitives.ErrConflict) {
			return err
		}
		_, err = wakeStore.UpsertAgentWakeup(ctx, primitives.AgentWakeup{
			WakeupID:         wakeupID,
			Status:           primitives.AgentWakeupStatusRequested,
			TargetHandle:     handle,
			TargetActorID:    actorID,
			WorkspaceID:      workspaceID,
			WorkspaceName:    workspaceName,
			ThreadID:         threadID,
			TriggerEventID:   askID,
			TriggerCreatedAt: ts,
			TriggerText:      text,
			Refs:             append(refs, "artifact:"+wakeupID),
		})
		return err
	}
}

// NewEscalationReadCheck reports whether a principal may read an ask event,
// using the same relational access policy as every scoped read.
func NewEscalationReadCheck(store *primitives.Store) func(context.Context, string, string) bool {
	if store == nil {
		return nil
	}
	return func(ctx context.Context, principalID, askID string) bool {
		if strings.TrimSpace(principalID) == "" {
			return false
		}
		scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: principalID})
		return store.CanAccessResource(scoped, "event", askID)
	}
}
