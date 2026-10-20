package storage_test

import (
	"context"
	"fmt"
	"testing"

	"agent-nexus-core/internal/primitives"
)

// One inbox item with a huge ref array used to be expanded in a single
// statement however small the item cap. The item phase now splits it across
// chunks by a ref budget, keeps it unready until its final slice, and then
// computes lifecycle from all of its refs.
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
	thread, err := store.CreateThread(ctx, "owner", map[string]any{"title": "Refs"})
	if err != nil {
		t.Fatal(err)
	}
	id := thread.Thread["id"].(string)
	const huge = 4500
	refs := make([]string, 0, huge)
	for i := 0; i < huge-1; i++ {
		refs = append(refs, fmt.Sprintf("card:ref-%05d", i))
	}
	// Only the last element names a hidden subject, beyond the first slices.
	refs = append(refs, "thread:hidden-subject")
	items := []primitives.DerivedInboxItem{
		{ID: "item-a", ThreadID: id, Category: "agent_wake", TriggerAt: "2026-10-10T00:00:00Z", Data: map[string]any{"related_refs": []string{"thread:" + id}}},
		{ID: "item-b", ThreadID: id, Category: "agent_wake", TriggerAt: "2026-10-10T00:00:00Z", Data: map[string]any{"related_refs": refs}},
		{ID: "item-c", ThreadID: id, Category: "agent_wake", TriggerAt: "2026-10-10T00:00:00Z", Data: map[string]any{"related_refs": []string{"card:small"}}},
	}
	if err = store.ReplaceDerivedInboxItems(ctx, id, items); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT OR IGNORE INTO inbox_hidden_subject_refs VALUES('thread','hidden-subject','thread:hidden-subject')`,
		`DELETE FROM inbox_lifecycle_refs`,
		`UPDATE derived_inbox_items SET lifecycle_ready=0,lifecycle_hidden=0`,
		`UPDATE inbox_lifecycle_job SET phase=5,cursor='',ref_offset=0,owners_ready=1,done=0`,
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
	before, chunks := 0, 0
	for done := false; !done; chunks++ {
		if chunks > 20 {
			t.Fatal("item phase did not converge")
		}
		if done, err = ws.MaintainInboxLifecycleBatch(ctx, 200); err != nil {
			t.Fatal(err)
		}
		after := count(`SELECT count(*) FROM inbox_lifecycle_refs`)
		if after-before > 2000+2 {
			t.Fatalf("chunk %d expanded %d refs", chunks, after-before)
		}
		before = after
		if !done && count(`SELECT count(*) FROM inbox_lifecycle_refs WHERE inbox_id='item-b'`) < huge && count(`SELECT lifecycle_ready FROM derived_inbox_items WHERE id='item-b'`) != 0 {
			t.Fatalf("chunk %d marked a partially indexed item ready", chunks)
		}
	}
	if chunks < 3 {
		t.Fatalf("huge item was not split: %d chunks", chunks)
	}
	if n := count(`SELECT count(*) FROM inbox_lifecycle_refs WHERE inbox_id='item-b'`); n != huge {
		t.Fatalf("item-b indexed %d of %d refs", n, huge)
	}
	if count(`SELECT count(*) FROM derived_inbox_items WHERE lifecycle_ready=0`) != 0 {
		t.Fatal("items left unready")
	}
	if count(`SELECT lifecycle_hidden FROM derived_inbox_items WHERE id='item-b'`) != 1 || count(`SELECT lifecycle_hidden FROM derived_inbox_items WHERE id='item-c'`) != 0 {
		t.Fatal("lifecycle not computed from all refs")
	}
	if count(`SELECT ref_offset FROM inbox_lifecycle_job WHERE singleton=1`) != 0 || count(`SELECT done FROM inbox_lifecycle_job WHERE singleton=1`) != 1 {
		t.Fatal("job state not finished")
	}
}
