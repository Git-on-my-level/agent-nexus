package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

// A slice's write transaction must cost the same for a 50k-ref item as for
// a small one: the array is expanded in a read snapshot, and the write only
// inserts the budget-sized slice and updates the one-row job. Cost is SQLite
// VM work (triggers included) and WAL frames written, which machine load
// cannot inflate.
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
	if _, err = db.ExecContext(ctx, `DELETE FROM inbox_lifecycle_refs`); err != nil {
		t.Fatal(err)
	}
	pool, capture, err := perfguard.Open(sqliteDSN(w.Layout().DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := &Workspace{db: pool}
	walFrames := func() int {
		t.Helper()
		var busy, frames, checkpointed int
		if err := db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`).Scan(&busy, &frames, &checkpointed); err != nil {
			t.Fatal(err)
		}
		return frames
	}
	measure := func(id string) (perfguard.WorkStats, int) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE inbox_lifecycle_job SET split_id=?,split_offset=0,split_hidden=0`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			t.Fatal(err)
		}
		slice, err := w.readInboxLifecycleSlice(ctx, id, 0, inboxLifecycleRefBudget)
		if err != nil {
			t.Fatal(err)
		}
		before := walFrames()
		capture.Start()
		err = measured.writeInboxLifecycleSlice(ctx, id, 0, inboxLifecycleRefBudget, slice)
		statements, queries, rows := capture.Stop()
		frames := walFrames() - before
		if err != nil || capture.WorkError() != nil {
			t.Fatal(err, capture.WorkError())
		}
		work := capture.Work()
		// Keep the absolute bounds and payload-read guards from main as well
		// as the relative work and WAL-frame checks below.
		if queries > 7 || rows > 3 || work.VMSteps > uint64(inboxLifecycleRefBudget*40) {
			t.Fatalf("unbounded slice: SQL=%d rows=%d work=%+v", queries, rows, work)
		}
		for _, statement := range statements {
			sql := strings.ToLower(strings.Join(strings.Fields(statement.SQL), " "))
			if strings.Contains(sql, "data_json") || strings.Contains(sql, "select *") || strings.Contains(sql, "json_array_length") {
				t.Fatalf("write reloaded or sized the stored payload: %s", statement.SQL)
			}
		}
		var n int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM inbox_lifecycle_refs WHERE inbox_id=?`, id).Scan(&n); err != nil || n != inboxLifecycleRefBudget {
			t.Fatalf("%s slice indexed %d refs: %v", id, n, err)
		}
		return capture.Work(), frames
	}
	// Interleave repeated slices so both items exercise the same checkpoint
	// and B-tree states. Scheduling is not part of the acceptance criteria.
	for i := 0; i < 9; i++ {
		small, smallFrames := measure("small")
		big, bigFrames := measure("big")
		t.Logf("slice write: small item %+v frames=%d, 50k-ref item %+v frames=%d", small, smallFrames, big, bigFrames)
		// Expanding the 50k array inside the write changes VM work; rewriting
		// its ~1 MB row adds about 250 frames. Retain main's exact VM check
		// and allow only B-tree split noise in the physical-write bound.
		if big.VMSteps != small.VMSteps || big.FullScanSteps > small.FullScanSteps+small.FullScanSteps/10 || bigFrames > smallFrames+smallFrames/2 {
			t.Fatalf("slice write grows with item size: small %+v frames=%d, big %+v frames=%d", small, smallFrames, big, bigFrames)
		}
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

// A rewrite that keeps data_json's byte length (here: many more, shorter
// refs) must not be indexed inline with the stale planned ref count.
func TestInboxLifecycleSameLengthRewriteIsNotIndexedInline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	db := w.DB()
	document := func(refs []string, size int) string {
		t.Helper()
		encode := func(pad int) string {
			data, err := json.Marshal(map[string]any{"related_refs": refs, "pad": strings.Repeat("x", pad)})
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
		base := encode(0)
		if size == 0 {
			return base
		}
		if len(base) > size {
			t.Fatalf("cannot pad %d bytes to %d", len(base), size)
		}
		return encode(size - len(base)) // each pad byte adds one byte
	}
	short := make([]string, 3000)
	for i := range short {
		short[i] = fmt.Sprintf("card:s%04d", i)
	}
	rewritten := document(short, 0)
	long := make([]string, 20)
	for i := range long {
		long[i] = "card:" + strings.Repeat(fmt.Sprintf("%c", 'a'+i), 900)
	}
	original := document(long, len(rewritten))
	if len(original) != len(rewritten) || len(original) > inboxLifecycleInlineBytes {
		t.Fatalf("fixture sizes %d and %d", len(original), len(rewritten))
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('item','thread-x','agent_wake','now','now',?)`, []any{original}},
		{`DELETE FROM inbox_lifecycle_refs`, nil},
		{`UPDATE derived_inbox_items SET lifecycle_ready=0`, nil},
		{`UPDATE inbox_lifecycle_job SET phase=5,cursor='',split_id='',split_offset=0,split_hidden=0,owners_ready=1,done=0`, nil},
	} {
		if _, err = db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	planned, err := w.classifyInboxLifecycleItems(ctx, "", 200)
	if err != nil || len(planned) != 1 || planned[0].refs != 20 {
		t.Fatalf("plan: %+v %v", planned, err)
	}
	// Same byte length and projection version; only the ref count changes.
	if _, err = db.ExecContext(ctx, `UPDATE derived_inbox_items SET data_json=? WHERE id='item'`, rewritten); err != nil {
		t.Fatal(err)
	}
	// The update trigger reindexed it fully; clear its refs to observe
	// whether the stale plan indexes it again inside the write.
	if _, err = db.ExecContext(ctx, `DELETE FROM inbox_lifecycle_refs`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	cursor, _, err := maintainInboxLifecycleItems(ctx, tx, "", planned, 200, inboxLifecycleRefBudget)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM inbox_lifecycle_refs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 || cursor != "item" {
		t.Fatalf("stale candidate re-indexed inline: refs=%d cursor=%q", n, cursor)
	}
}
