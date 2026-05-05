package primitives_test

import (
	"agent-nexus-core/internal/blob"
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestAgentWakeupRefsCorruptionReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	wakeupID := "wake-corrupt-refs"
	if _, err := store.UpsertAgentWakeup(ctx, primitives.AgentWakeup{
		WakeupID:       wakeupID,
		TargetHandle:   "target.agent",
		TargetActorID:  "actor-target",
		WorkspaceID:    "ws_main",
		WorkspaceName:  "Main",
		ThreadID:       "thread-corrupt-refs",
		TriggerEventID: "event-corrupt-refs",
		Refs:           []string{"thread:thread-corrupt-refs", "event:event-corrupt-refs"},
	}); err != nil {
		t.Fatalf("seed wakeup: %v", err)
	}

	if _, err := workspace.DB().ExecContext(ctx, `UPDATE agent_wakeups SET refs_json = ? WHERE wakeup_id = ?`, `{not-json`, wakeupID); err != nil {
		t.Fatalf("corrupt wakeup refs_json: %v", err)
	}

	if _, err := store.GetAgentWakeup(ctx, wakeupID); err == nil || !strings.Contains(err.Error(), "decode agent_wakeup.refs") {
		t.Fatalf("expected get wakeup refs decode error, got %v", err)
	}
	if _, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: "actor-target"}); err == nil || !strings.Contains(err.Error(), "decode agent_wakeup.refs") {
		t.Fatalf("expected list wakeup refs decode error, got %v", err)
	}
}
