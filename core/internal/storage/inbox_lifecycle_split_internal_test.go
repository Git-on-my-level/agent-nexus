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

// The write chunk's discovery step must not size items itself: the plan is
// read outside the lock, and the write only re-checks byte lengths and marks
// an oversized first item as the split item.
func TestInboxLifecycleDiscoveryWriteIsBounded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	db := w.DB()
	if _, err = db.ExecContext(ctx, `DROP TRIGGER inbox_lifecycle_insert`); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{"a-big": 50000, "b-small": 500} {
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
	plan := func(cursor string) []inboxLifecycleCandidate {
		t.Helper()
		candidates, err := w.classifyInboxLifecycleItems(ctx, cursor, 1)
		if err != nil || len(candidates) != 1 {
			t.Fatalf("plan after %q: %v %v", cursor, candidates, err)
		}
		return candidates
	}
	measure := func(cursor string, candidates []inboxLifecycleCandidate) time.Duration {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		start := time.Now()
		if _, _, err = maintainInboxLifecycleItems(ctx, tx, cursor, candidates, 1, inboxLifecycleRefBudget); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}
	big, small := plan(""), plan("a-big")
	if big[0].refs != 50000 || small[0].refs != 500 {
		t.Fatalf("classification: %+v %+v", big, small)
	}
	var bigs, smalls []time.Duration
	for i := 0; i < 9; i++ {
		bigs = append(bigs, measure("", big))
		smalls = append(smalls, measure("a-big", small))
	}
	median := func(runs []time.Duration) time.Duration {
		sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
		return runs[len(runs)/2]
	}
	b, s := median(bigs), median(smalls)
	t.Logf("write chunk: mark 50k-ref item for split %s, index a 500-ref item inline %s", b, s)
	if b > 3*s+10*time.Millisecond {
		t.Fatalf("discovery in the write grows with item size: big %s, small %s", b, s)
	}
}

// Renaming a split item ends its split, and a slice for an id that no
// longer exists never inserts refs, even if the split state survived.
func TestInboxLifecycleSplitSurvivesRename(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	db := w.DB()
	refs := make([]string, 3000)
	for i := range refs {
		refs[i] = fmt.Sprintf("card:r-%05d", i)
	}
	data, err := json.Marshal(map[string]any{"related_refs": refs})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('old-id','thread-x','agent_wake','now','now',?)`, []any{string(data)}},
		{`DELETE FROM inbox_lifecycle_refs`, nil},
		{`UPDATE derived_inbox_items SET lifecycle_ready=0`, nil},
		{`UPDATE inbox_lifecycle_job SET phase=5,cursor='',split_id='',split_offset=0,split_hidden=0,owners_ready=1,done=0`, nil},
	} {
		if _, err = db.ExecContext(ctx, q.sql, q.args...); err != nil {
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
	for i := 0; i < 2; i++ { // mark the split, then index its first slice
		if _, err = w.MaintainInboxLifecycleBatch(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	if count(`SELECT split_offset FROM inbox_lifecycle_job WHERE split_id='old-id'`) != inboxLifecycleRefBudget {
		t.Fatal("first slice not indexed")
	}
	stale, err := w.readInboxLifecycleSlice(ctx, "old-id", inboxLifecycleRefBudget, 2*inboxLifecycleRefBudget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE derived_inbox_items SET id='new-id' WHERE id='old-id'`); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT count(*) FROM inbox_lifecycle_job WHERE split_id<>''`) != 0 {
		t.Fatal("rename left the split state on the old id")
	}
	// Simulate split state that survived (for example a trigger installed by
	// an older build); the writer itself must refuse the missing id.
	if _, err = db.ExecContext(ctx, `UPDATE inbox_lifecycle_job SET split_id='old-id',split_offset=?`, inboxLifecycleRefBudget); err != nil {
		t.Fatal(err)
	}
	if err = w.writeInboxLifecycleSlice(ctx, "old-id", inboxLifecycleRefBudget, inboxLifecycleRefBudget, stale); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM inbox_lifecycle_refs WHERE inbox_id='old-id'`); n != 0 {
		t.Fatalf("stale slice inserted %d orphan refs", n)
	}
	if count(`SELECT count(*) FROM inbox_lifecycle_job WHERE split_id<>''`) != 0 {
		t.Fatal("writer kept split state for a missing id")
	}
	if n := count(`SELECT count(*) FROM inbox_lifecycle_refs WHERE inbox_id='new-id'`); n != 3000 {
		t.Fatalf("renamed item indexed %d refs", n)
	}
}

// Indexing an item's refs (live writes and the inline backfill path) must be
// linear in its ref count. The explicit-refs test used to be re-evaluated
// for every expanded element, which was quadratic.
func TestInboxLifecycleRefIndexingIsLinear(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	insert := func(id string, size int, field string) time.Duration {
		t.Helper()
		refs := make([]string, size)
		for i := range refs {
			refs[i] = fmt.Sprintf("card:%s-%06d", id, i)
		}
		data, err := json.Marshal(map[string]any{field: refs})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if _, err = w.DB().ExecContext(ctx, `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES(?,'thread-x','agent_wake','now','now',?)`, id, string(data)); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		var n int
		if err = w.DB().QueryRowContext(ctx, `SELECT count(*) FROM inbox_lifecycle_refs WHERE inbox_id=?`, id).Scan(&n); err != nil || n < size {
			t.Fatalf("%s indexed %d of %d refs: %v", id, n, size, err)
		}
		return elapsed
	}
	for _, field := range []string{"related_refs", "refs"} {
		small := insert(field+"-small", 2000, field)
		big := insert(field+"-big", 20000, field)
		t.Logf("%s: 2k refs %s, 20k refs %s", field, small, big)
		// Linear is about 10x; the quadratic form was about 100x.
		if big > 30*small+200*time.Millisecond {
			t.Fatalf("%s indexing is superlinear: 2k %s, 20k %s", field, small, big)
		}
	}
}
