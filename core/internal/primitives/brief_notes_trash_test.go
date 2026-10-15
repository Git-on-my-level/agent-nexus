package primitives_test

import (
	"context"
	"database/sql"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestTrashedNoteLeavesThePromiseAndRestorePutsItBack(t *testing.T) {
	t.Parallel()
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	ctx := context.Background()
	board, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	card := registerWork(t, s, board["id"].(string))
	cardID := card["id"].(string)
	db := ws.DB()
	if _, err := s.RefreshBriefInitiatives(ctx, 200); err != nil {
		t.Fatal(err)
	}
	posted, err := s.AppendEvent(ctx, "agent-1", map[string]any{
		"type": "message_posted", "thread_id": card["thread_id"],
		"refs":    []string{"thread:" + card["thread_id"].(string)},
		"payload": map[string]any{"text": "progress", "next": map[string]any{"at": "2026-08-01", "what": "ship the release"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	eventID := posted["id"].(string)
	if _, err := s.RefreshBriefInitiatives(ctx, 200); err != nil {
		t.Fatal(err)
	}
	if what := promiseWhat(t, db, cardID); what != "ship the release" {
		t.Fatalf("promise what %q", what)
	}

	if _, err := s.TrashEvent(ctx, "actor-1", eventID, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	if n := noteCount(t, db, eventID); n != 0 {
		t.Fatalf("trashed note still stored: %d", n)
	}
	if dirty := dirtyCount(t, db, cardID); dirty != 1 {
		t.Fatalf("trash did not dirty the card: %d", dirty)
	}
	if _, err := s.RefreshBriefInitiatives(ctx, 200); err != nil {
		t.Fatal(err)
	}
	if what := promiseWhat(t, db, cardID); what != "" {
		t.Fatalf("trashed note still drives the promise: %q", what)
	}

	if _, err := s.RestoreEvent(ctx, "actor-1", eventID); err != nil {
		t.Fatal(err)
	}
	if n := noteCount(t, db, eventID); n != 1 {
		t.Fatalf("restored note missing: %d", n)
	}
	if dirty := dirtyCount(t, db, cardID); dirty != 1 {
		t.Fatalf("restore did not dirty the card: %d", dirty)
	}
	if _, err := s.RefreshBriefInitiatives(ctx, 200); err != nil {
		t.Fatal(err)
	}
	if what := promiseWhat(t, db, cardID); what != "ship the release" {
		t.Fatalf("restored promise what %q", what)
	}
}

func promiseWhat(t *testing.T, db *sql.DB, cardID string) string {
	t.Helper()
	var what string
	err := db.QueryRow(`SELECT COALESCE(promise_what,'') FROM brief_initiatives WHERE card_id=?`, cardID).Scan(&what)
	if err != nil {
		return ""
	}
	return what
}

func noteCount(t *testing.T, db *sql.DB, eventID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM brief_notes WHERE event_id=?`, eventID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dirtyCount(t *testing.T, db *sql.DB, cardID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM brief_dirty WHERE card_id=?`, cardID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
