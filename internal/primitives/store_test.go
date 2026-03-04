package primitives_test

import (
	"context"
	"testing"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/storage"
)

func TestStoreAppendAndGetEventUnknownTypeAccepted(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

	event, err := store.AppendEvent(context.Background(), "actor-1", map[string]any{
		"type":       "custom_event_type",
		"refs":       []any{"customprefix:abc"},
		"summary":    "custom event",
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}

	loaded, err := store.GetEvent(context.Background(), event["id"].(string))
	if err != nil {
		t.Fatalf("get event: %v", err)
	}

	if loaded["type"] != "custom_event_type" {
		t.Fatalf("unexpected event type: %#v", loaded["type"])
	}
}
