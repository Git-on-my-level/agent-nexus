package primitives_test

import (
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"context"
	"errors"
	"testing"
)

func TestArchiveLifecycleFencesRejectStaleSnapshots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	topic, err := store.CreateTopic(ctx, "actor", map[string]any{"title": "Fenced topic", "summary": "initial"})
	if err != nil {
		t.Fatal(err)
	}
	topicID := topic.Topic["id"].(string)
	topicSnapshot := topic.Topic["updated_at"].(string)
	if _, err := store.PatchTopic(ctx, "actor", topicID, map[string]any{"summary": "changed"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveTopicIfUpdatedAt(ctx, "actor", topicID, &topicSnapshot); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("stale topic archive returned %v, want conflict", err)
	}

	board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Fenced board"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := board["id"].(string)
	boardSnapshot := board["updated_at"].(string)
	if _, err := store.UpdateBoard(ctx, "actor", boardID, map[string]any{"summary": "changed"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveBoardIfUpdatedAt(ctx, "actor", boardID, &boardSnapshot); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("stale board archive returned %v, want conflict", err)
	}

	document, _, err := store.CreateDocument(ctx, "actor", map[string]any{"title": "Fenced document"}, "snapshot", "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	documentID := document["id"].(string)
	documentSnapshot := document["updated_at"].(string)
	if _, _, err := store.PatchDocument(ctx, "actor", documentID, map[string]any{"summary": "changed"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ArchiveDocumentIfUpdatedAt(ctx, "actor", documentID, &documentSnapshot); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("stale document archive returned %v, want conflict", err)
	}

	work, err := store.CreateWork(ctx, "actor", "", map[string]any{"title": "Fenced card", "source": map[string]any{"authority": "nexus"}})
	if err != nil {
		t.Fatal(err)
	}
	cardID := work["id"].(string)
	cardVersion := work["version"].(int64)
	if _, err := store.PatchWork(ctx, "actor", cardID, cardVersion, map[string]any{"priority": "p1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveBoardCard(ctx, "actor", "", cardID, primitives.RemoveBoardCardInput{IfWorkVersion: &cardVersion}); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("stale card archive returned %v, want conflict", err)
	}
}
