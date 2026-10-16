package primitives_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"

	"github.com/google/uuid"
)

func TestCreateBoardCardClassifiesClientErrors(t *testing.T) {
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "actor-1"})

	threadID := createBoardTestThread(t, scoped, store, "Lane thread")
	board, err := store.CreateBoard(scoped, "actor-1", map[string]any{
		"title":     "omi lanes",
		"thread_id": threadID,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)

	_, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
		Title:     "Bad column",
		Body:      "test",
		ColumnKey: "not-a-column",
	})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) || outcomeUnknown(err) {
		t.Fatalf("invalid column: %v", err)
	}

	_, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{Title: "bad\x00title", Body: "test"})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("nul title: %v", err)
	}
	_, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{Title: string([]byte{'b', 'a', 'd', 0xff}), Body: "test"})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid utf-8 title: %v", err)
	}

	_, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
		Title: "Bad ref",
		Refs:  []string{"not a ref"},
	})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) || !strings.Contains(err.Error(), "typed ref") {
		t.Fatalf("malformed ref: %v", err)
	}

	_, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
		Title:          "Missing thread",
		ParentThreadID: uuid.NewString(),
	})
	if !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("unknown thread: %v", err)
	}

	missingDoc := uuid.NewString()
	_, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
		Title:            "Missing document",
		PinnedDocumentID: &missingDoc,
	})
	if !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("unknown document: %v", err)
	}

	if _, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
		Title: "Open prefix",
		Refs:  []string{"widget:abc"},
	}); err != nil {
		t.Fatalf("unknown ref prefix: %v", err)
	}

	for i := 0; i < 21; i++ {
		if _, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
			Title: fmt.Sprintf("card %d", i),
			Body:  "body",
		}); err != nil {
			t.Fatalf("card %d: %v", i, err)
		}
	}
	created, err := store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
		Title: "Loop: storage-auto",
		Body:  "test",
	})
	if err != nil {
		t.Fatalf("plain create: %v", err)
	}
	if created.Card["title"] != "Loop: storage-auto" {
		t.Fatalf("card title %#v", created.Card["title"])
	}

	if _, err = workspace.DB().ExecContext(ctx, `UPDATE boards SET column_schema_json = ? WHERE id = ?`, `[{"key":"ready","title":"Ready"}]`, boardID); err != nil {
		t.Fatalf("narrow columns: %v", err)
	}
	_, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
		Title:     "Off schema",
		ColumnKey: "backlog",
	})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) || !strings.Contains(err.Error(), "not on this board") || outcomeUnknown(err) {
		t.Fatalf("column missing from board: %v", err)
	}
	if _, err = store.CreateBoardCard(scoped, "actor-1", boardID, primitives.AddBoardCardInput{
		Title:     "On schema",
		ColumnKey: "ready",
	}); err != nil {
		t.Fatalf("ready column: %v", err)
	}
}

func outcomeUnknown(err error) bool {
	var unknown *primitives.MutationOutcomeUnknown
	return errors.As(err, &unknown)
}
