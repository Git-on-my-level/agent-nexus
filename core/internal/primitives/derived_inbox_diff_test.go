package primitives

import (
	"context"
	"testing"
	"time"
)

// A refresh that derives the same rows writes nothing: no trigger runs and
// the access epoch stays put, so cached readability checks stay valid. A
// changed row is replaced and a row no longer derived is removed.
func TestReplaceDerivedInboxItemsWritesOnlyChangedRows(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	created, err := store.CreateThread(ctx, "owner", map[string]any{"title": "Diff"})
	if err != nil {
		t.Fatal(err)
	}
	thread := created.Thread["id"].(string)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	item := func(id, title string) DerivedInboxItem {
		return DerivedInboxItem{ID: id, ThreadID: thread, Category: "ask", TriggerAt: at, Data: map[string]any{"kind": "ask", "title": title, "subject_ref": "thread:" + thread}}
	}
	epoch := func() int64 {
		t.Helper()
		var v int64
		if err := ws.DB().QueryRowContext(ctx, `SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	rowid := func(id string) int64 {
		t.Helper()
		var v int64
		if err := ws.DB().QueryRowContext(ctx, `SELECT rowid FROM derived_inbox_items WHERE id=?`, id).Scan(&v); err != nil {
			t.Fatalf("row %s: %v", id, err)
		}
		return v
	}
	if err := store.ReplaceDerivedInboxItems(ctx, thread, []DerivedInboxItem{item("a", "A"), item("b", "B")}); err != nil {
		t.Fatal(err)
	}
	before, rowA := epoch(), rowid("a")
	if err := store.ReplaceDerivedInboxItems(ctx, thread, []DerivedInboxItem{item("a", "A"), item("b", "B")}); err != nil {
		t.Fatal(err)
	}
	if got := epoch(); got != before {
		t.Fatalf("unchanged refresh bumped the access epoch %d -> %d", before, got)
	}
	if rowid("a") != rowA {
		t.Fatal("unchanged row was rewritten")
	}
	if err := store.ReplaceDerivedInboxItems(ctx, thread, []DerivedInboxItem{item("a", "A2")}); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := ws.DB().QueryRowContext(ctx, `SELECT json_extract(data_json,'$.title') FROM derived_inbox_items WHERE id='a'`).Scan(&title); err != nil || title != "A2" {
		t.Fatalf("changed row: %q %v", title, err)
	}
	var n int
	if err := ws.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM derived_inbox_items WHERE id='b'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("removed row still present: %d %v", n, err)
	}
}
