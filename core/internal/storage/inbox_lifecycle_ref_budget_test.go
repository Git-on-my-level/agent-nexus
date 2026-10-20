package storage_test

import (
	"context"
	"fmt"
	"testing"

	"agent-nexus-core/internal/primitives"
)

// One inbox item with a huge ref array used to be expanded in a single
// statement however small the item cap. The item phase now splits it across
// chunks by a ref budget and keeps it unready until its final slice, even
// when a subject is unhidden mid-split, then computes lifecycle from all of
// its refs.
func TestInboxLifecycleSplitsHugeRefArrays(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	store := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	newThread := func(title string) string {
		t.Helper()
		thread, err := store.CreateThread(ctx, "owner", map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return thread.Thread["id"].(string)
	}
	home, early, late := newThread("Refs"), newThread("Early subject"), newThread("Late subject")
	for _, id := range []string{early, late} {
		if _, err = store.ArchiveThread(ctx, "owner", id); err != nil {
			t.Fatal(err)
		}
	}
	const huge = 4500
	bulk := func(first, last string) []string {
		refs := []string{first}
		for i := 1; i < huge-1; i++ {
			refs = append(refs, fmt.Sprintf("card:ref-%05d", i))
		}
		return append(refs, last)
	}
	items := []primitives.DerivedInboxItem{
		{ID: "item-a", ThreadID: home, Category: "agent_wake", TriggerAt: "2026-10-10T00:00:00Z", Data: map[string]any{"related_refs": []string{"thread:" + home}}},
		// Hidden at its first and last element; the first is unhidden mid-split.
		{ID: "item-b", ThreadID: home, Category: "agent_wake", TriggerAt: "2026-10-10T00:00:00Z", Data: map[string]any{"related_refs": bulk("thread:"+early, "thread:"+late)}},
		{ID: "item-c", ThreadID: home, Category: "agent_wake", TriggerAt: "2026-10-10T00:00:00Z", Data: map[string]any{"related_refs": []string{"card:small"}}},
		// Hidden only through its first element, which is unhidden mid-split.
		{ID: "item-d", ThreadID: home, Category: "agent_wake", TriggerAt: "2026-10-10T00:00:00Z", Data: map[string]any{"related_refs": bulk("thread:"+early, "card:tail")}},
	}
	if err = store.ReplaceDerivedInboxItems(ctx, home, items); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DELETE FROM inbox_lifecycle_refs`,
		`UPDATE derived_inbox_items SET lifecycle_ready=0,lifecycle_hidden=0`,
		`UPDATE inbox_lifecycle_job SET phase=5,cursor='',split_id='',split_offset=0,split_hidden=0,owners_ready=1,done=0`,
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	unhidden := false
	before, chunks := 0, 0
	for done := false; !done; chunks++ {
		if chunks > 40 {
			t.Fatal("item phase did not converge")
		}
		if done, err = ws.MaintainInboxLifecycleBatch(ctx, 200); err != nil {
			t.Fatal(err)
		}
		after := count(`SELECT count(*) FROM inbox_lifecycle_refs`)
		if after-before > 2000+2 {
			t.Fatalf("chunk %d indexed %d refs", chunks, after-before)
		}
		before = after
		var split string
		var offset int
		if err = db.QueryRowContext(ctx, `SELECT split_id,split_offset FROM inbox_lifecycle_job WHERE singleton=1`).Scan(&split, &offset); err != nil {
			t.Fatal(err)
		}
		if split != "" && count(`SELECT lifecycle_ready FROM derived_inbox_items WHERE id=?`, split) != 0 {
			t.Fatalf("chunk %d: split item %s is ready", chunks, split)
		}
		if split == "item-b" && offset > 0 && !unhidden {
			// Unhide the subject of an already-indexed ref. The owner trigger
			// sees the split item as dirty but must not mark it ready.
			if _, err = store.UnarchiveThread(ctx, "owner", early); err != nil {
				t.Fatal(err)
			}
			unhidden = true
			if count(`SELECT lifecycle_ready FROM derived_inbox_items WHERE id='item-b'`) != 0 {
				t.Fatal("unhide marked a partially indexed item ready")
			}
		}
	}
	if !unhidden || chunks < 6 {
		t.Fatalf("items were not split: chunks=%d unhidden=%v", chunks, unhidden)
	}
	for _, id := range []string{"item-b", "item-d"} {
		if n := count(`SELECT count(*) FROM inbox_lifecycle_refs WHERE inbox_id=?`, id); n != huge {
			t.Fatalf("%s indexed %d of %d refs", id, n, huge)
		}
	}
	if count(`SELECT count(*) FROM derived_inbox_items WHERE lifecycle_ready=0`) != 0 {
		t.Fatal("items left unready")
	}
	for id, want := range map[string]int{"item-a": 0, "item-b": 1, "item-c": 0, "item-d": 0} {
		if got := count(`SELECT lifecycle_hidden FROM derived_inbox_items WHERE id=?`, id); got != want {
			t.Fatalf("%s hidden=%d, want %d", id, got, want)
		}
	}
	var split string
	if err = db.QueryRowContext(ctx, `SELECT split_id FROM inbox_lifecycle_job WHERE singleton=1`).Scan(&split); err != nil || split != "" || count(`SELECT done FROM inbox_lifecycle_job WHERE singleton=1`) != 1 {
		t.Fatalf("job state not finished: split=%q err=%v", split, err)
	}
}
