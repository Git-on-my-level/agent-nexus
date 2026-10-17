package primitives_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/primitives"
)

func TestCardCreateChosenHandleConflictAndAutoLength(t *testing.T) {
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Tasks"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := board["id"].(string)
	first, err := store.CreateBoardCard(ctx, "actor-1", boardID, primitives.AddBoardCardInput{Handle: "chosen", Title: "Ship", Body: "Details", ColumnKey: "ready"})
	if err != nil || first.Card["handle"] != "chosen" || first.Card["ref"] != "card:chosen" {
		t.Fatalf("%#v %v", first, err)
	}
	_, err = store.CreateBoardCard(ctx, "actor-1", boardID, primitives.AddBoardCardInput{Handle: "chosen", Title: "Another", ColumnKey: "ready"})
	if !errors.Is(err, primitives.ErrCardHandleConflict) {
		t.Fatalf("conflict=%v", err)
	}
	for _, bad := range []string{"UPPER", "two--dashes", "cards", "-edge", strings.Repeat("x", 65), "5c04521b-7629-4ef2-88ee-3dac188b5837"} {
		_, err = store.CreateBoardCard(ctx, "actor-1", boardID, primitives.AddBoardCardInput{Handle: bad, Title: "Invalid", ColumnKey: "ready"})
		if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	for i := 0; i < 12; i++ {
		created, err := store.CreateBoardCard(ctx, "actor-1", boardID, primitives.AddBoardCardInput{Title: strings.Repeat("long title words ", 10), ColumnKey: "ready"})
		if err != nil || len(created.Card["handle"].(string)) > 40 {
			t.Fatalf("%#v %v", created, err)
		}
	}
	for _, name := range []string{"cards", "new", "threads", "api", "attachments", "documents", "events", "search", "settings", "topics", "trash", "uuid"} {
		for i := 0; i < 2; i++ {
			created, err := store.CreateBoardCard(ctx, "actor-1", boardID, primitives.AddBoardCardInput{Title: name + " " + strings.Repeat("x", 40), ColumnKey: "ready"})
			if err != nil {
				t.Fatal(err)
			}
			handle := created.Card["handle"].(string)
			if !handles.ValidSlug(handle) || len(handle) > 40 {
				t.Fatalf("derived %s: %q", name, handle)
			}
			resolved, err := store.ResolveResourceRef(ctx, primitives.ResourceRefInput{Ref: created.Card["ref"].(string)})
			if err != nil || resolved.ID != created.Card["id"] {
				t.Fatalf("unresolvable derived ref: %#v %v", created.Card, err)
			}
		}
	}
	existing, err := store.GetBoardCard(ctx, "", first.Card["id"].(string))
	if err != nil || existing["handle"] != "chosen" {
		t.Fatalf("existing=%#v %v", existing, err)
	}
}
