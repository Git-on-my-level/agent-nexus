package primitives

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDerivedInboxReplacementRejectsNestedPrivateIdentity(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := store.CreateThread(ctx, "owner", map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateThread(ctx, "owner", map[string]any{"title": "Private", "pm_actor_id": "owner"})
	if err != nil {
		t.Fatal(err)
	}
	thread := public.Thread["id"].(string)
	item := DerivedInboxItem{ID: "nested-private", ThreadID: thread, Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "ask", "subject_ref": "thread:" + thread, "metadata": map[string]any{"owner_ref": private.Thread["id"]}}}
	scoped := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
	if err := store.ReplaceDerivedInboxItems(scoped, thread, []DerivedInboxItem{item}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected private identity denial, got %v", err)
	}
	var count int
	if err := ws.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM derived_inbox_items WHERE id=?`, item.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("denied item persisted: count=%d err=%v", count, err)
	}
}
