package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"
)

// A slice's write transaction must cost the same for a 50k-ref item as for
// a small one: the array is expanded in a read snapshot, and the write only
// inserts the budget-sized slice and updates the one-row job.
func TestInboxLifecycleSliceWriteIsBoundedByBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	db := w.DB()
	// Seed the items as legacy rows: the write trigger's own full reindex of
	// a 50k array is not what this test measures.
	if _, err = db.ExecContext(ctx, `DROP TRIGGER inbox_lifecycle_insert`); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{"big": 50000, "small": 3000} {
		refs := make([]string, size)
		for i := range refs {
			refs[i] = fmt.Sprintf("card:%s-%06d", name, i)
		}
		data, err := json.Marshal(map[string]any{"related_refs": refs})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES(?,'thread-x','agent_wake','now','now',?)`, name, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	// Start both measurements from the same checkpointed state.
	for _, q := range []string{`DELETE FROM inbox_lifecycle_refs`, `PRAGMA wal_checkpoint(TRUNCATE)`} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	measure := func(id string) time.Duration {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE inbox_lifecycle_job SET split_id=?,split_offset=0,split_hidden=0`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM inbox_lifecycle_refs WHERE inbox_id=?`, id); err != nil {
			t.Fatal(err)
		}
		slice, err := w.readInboxLifecycleSlice(ctx, id, 0, inboxLifecycleRefBudget)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if err = w.writeInboxLifecycleSlice(ctx, id, 0, inboxLifecycleRefBudget, slice); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		var n int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM inbox_lifecycle_refs WHERE inbox_id=?`, id).Scan(&n); err != nil || n != inboxLifecycleRefBudget {
			t.Fatalf("%s slice indexed %d refs: %v", id, n, err)
		}
		return elapsed
	}
	// Interleave the two items so checkpoints and machine load hit both.
	var smalls, bigs []time.Duration
	for i := 0; i < 9; i++ {
		smalls = append(smalls, measure("small"))
		bigs = append(bigs, measure("big"))
	}
	median := func(runs []time.Duration) time.Duration {
		sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
		return runs[len(runs)/2]
	}
	small, big := median(smalls), median(bigs)
	t.Logf("slice write: small item %s, 50k-ref item %s", small, big)
	// Expanding the 50k array inside the write would cost far more than the
	// small item's 3k array; a bounded slice costs about the same.
	if big > 3*small+10*time.Millisecond {
		t.Fatalf("slice write grows with item size: small %s, big %s", small, big)
	}
}
