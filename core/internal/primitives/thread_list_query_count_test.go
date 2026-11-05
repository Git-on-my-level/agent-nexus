package primitives_test

import (
	"context"
	"fmt"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

// ListThreads resolves public ref handles for the whole page in one chunked
// batch. A bounded page must cost the same number of statements regardless of
// corpus size, and an unbounded list must not issue one lookup per row.
func TestListThreadsQueryCountIsBounded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	s := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)

	seed := func(from, to int) {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := from; i < to; i++ {
			id := fmt.Sprintf("count-thread-%04d", i)
			// Every row carries a resolvable self ref and a cross-row subject ref
			// so a per-row lookup would show up in the statement count.
			body := fmt.Sprintf(`{"title":"Thread %d","subject_ref":"thread:count-thread-%04d"}`, i, (i+1)%to)
			if _, err := tx.ExecContext(ctx, `INSERT INTO threads(id,handle,updated_at,updated_by,body_json) VALUES(?,?,?,'owner',?)`,
				id, fmt.Sprintf("h-%04d", i), fmt.Sprintf("2026-01-01T00:%02d:%02dZ", (i/60)%60, i%60), body); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	limit := 10
	pageCounts := map[int]int64{}
	seeded := 0
	for _, size := range []int{12, 450} {
		seed(seeded, size)
		seeded = size

		counter.Reset()
		page, cursor, err := s.ListThreads(ctx, primitives.ThreadListFilter{Limit: &limit})
		if err != nil {
			t.Fatal(err)
		}
		pageCounts[size] = counter.Count()
		if len(page) != limit || cursor == "" {
			t.Fatalf("size %d: page=%d cursor=%q", size, len(page), cursor)
		}
		for _, thread := range page {
			id := thread["id"].(string)
			if want := "thread:h-" + id[len("count-thread-"):]; thread["ref"] != want {
				t.Fatalf("thread %s ref=%v want %s", id, thread["ref"], want)
			}
			if subject, _ := thread["subject_ref"].(string); len(subject) < len("thread:h-") || subject[:len("thread:h-")] != "thread:h-" {
				t.Fatalf("thread %s subject_ref=%v was not rewritten to a public handle", id, thread["subject_ref"])
			}
		}

		counter.Reset()
		all, _, err := s.ListThreads(ctx, primitives.ThreadListFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != size {
			t.Fatalf("size %d: listed %d threads", size, len(all))
		}
		// One list statement plus chunked (200 ids) handle batches over the
		// distinct refs: each row has a handle self ref and an id subject ref.
		if got, max := counter.Count(), int64(1+(2*size+199)/200); got > max {
			t.Fatalf("unbounded list of %d threads: %d queries, want <= %d", size, got, max)
		}
	}
	if pageCounts[12] != pageCounts[450] {
		t.Fatalf("bounded page query count depends on corpus size: %v", pageCounts)
	}
	if pageCounts[12] > 2 {
		t.Fatalf("bounded page: %d queries, want <= 2", pageCounts[12])
	}
}
