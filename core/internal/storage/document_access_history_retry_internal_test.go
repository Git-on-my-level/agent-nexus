package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// legacyDocumentHistoryWorkspace holds per-revision edges written before the
// document history triggers existed, with the backfill job pending.
func legacyDocumentHistoryWorkspace(t *testing.T, revisions, refs int) *Workspace {
	t.Helper()
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	db := w.DB()
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='trigger' AND name LIKE 'access_document_history_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	for _, name := range names {
		if _, err = db.ExecContext(ctx, `DROP TRIGGER `+name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO documents(id,handle,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES('doc-a','doc-a','legacy',?,?,'t','owner','t','owner')`, fmt.Sprintf("rev-%d", revisions), revisions); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= revisions; i++ {
		var list []string
		for n := 0; n < refs; n++ {
			list = append(list, fmt.Sprintf(`"card:stable-%d-%d"`, i, n))
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO artifacts(id,handle,kind,created_at,created_by,content_type,content_hash,refs_json,metadata_json,content_refs_json) VALUES(?,?,'doc','t','owner','application/json','h','[]','{}',?)`, fmt.Sprintf("art-%d", i), fmt.Sprintf("art-%d", i), "["+strings.Join(list, ",")+"]"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,created_at,created_by) VALUES(?,'doc-a',?,?,'t','owner')`, fmt.Sprintf("rev-%d", i), i, fmt.Sprintf("art-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installDocumentAccessHistory(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return w
}

func perRevisionEdgeCount(t *testing.T, w *Workspace) int {
	t.Helper()
	var n int
	if err := w.DB().QueryRow(`SELECT count(*) FROM resource_access_edges WHERE source_kind IN ('artifact','document_revision') AND (source_id LIKE 'art-%' OR source_id LIKE 'rev-%')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A writer that keeps the database locked used to make every 50ms attempt
// fail with an unlogged error forever. Retries now back off, name the
// revision and error class, report a stuck cursor, and resume afterwards.
func TestDocumentAccessHistoryRetryBacksOffAndReportsStuckRevision(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := legacyDocumentHistoryWorkspace(t, 3, 4)
	before := perRevisionEdgeCount(t, w)
	if before == 0 {
		t.Fatal("fixture has no legacy edges")
	}
	holder, err := w.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	// Open the worker's physical connection before taking the lock. Otherwise
	// its connection-initialization PRAGMAs can wait on the writer before the
	// maintenance code has installed its short busy handler.
	warm, err := w.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = warm.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = holder.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	defer holder.ExecContext(context.Background(), `ROLLBACK`)
	type report struct {
		message string
		at      time.Time
	}
	reports := make(chan report, 5)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		w.runDocumentAccessHistoryMaintenance(ctx, func(err error) {
			select {
			case reports <- report{message: err.Error(), at: time.Now()}:
			case <-ctx.Done():
			}
		}, documentHistoryRetry{base: 10 * time.Millisecond, max: 80 * time.Millisecond, stuckAfter: 3})
	}()
	// Wait for actual attempts, not an assumed amount of scheduler time.
	// Backoff must never run early; scheduling delays have no upper bound.
	delays := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond, 80 * time.Millisecond}
	failures := make([]string, 0, len(delays))
	var previous time.Time
	deadline := time.After(10 * time.Second)
	for i, delay := range delays {
		select {
		case result := <-reports:
			failures = append(failures, result.message)
			if !strings.Contains(result.message, "in "+delay.String()+":") {
				t.Fatalf("attempt %d has wrong backoff: %s", i+1, result.message)
			}
			if i > 0 && result.at.Sub(previous) < delays[i-1] {
				t.Fatalf("attempt %d ran before its backoff: %s", i+1, result.at.Sub(previous))
			}
			previous = result.at
		case <-deadline:
			t.Fatalf("worker did not report the locked revision: %q", failures)
		}
	}
	if !strings.Contains(failures[0], "revision rev-1 (database busy)") {
		t.Fatalf("first report lacks revision and class: %s", failures[0])
	}
	if last := failures[len(failures)-1]; !strings.Contains(last, "stuck at revision rev-1 after") || !strings.Contains(last, "database busy") {
		t.Fatalf("no stuck report: %s", last)
	}
	if got := perRevisionEdgeCount(t, w); got != before {
		t.Fatalf("locked backfill moved edges: %d -> %d", before, got)
	}
	if _, err = holder.ExecContext(ctx, `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("backfill did not resume after the lock was released")
	}
	if got := perRevisionEdgeCount(t, w); got != 0 {
		t.Fatalf("%d per-revision edges left", got)
	}
}

// A budget that runs out after the revision row's edges must not start the
// artifact's pair; the cursor stays on the revision for the next chunk.
func TestDocumentAccessHistoryStopsBetweenRevisionSources(t *testing.T) {
	t.Parallel()
	w := legacyDocumentHistoryWorkspace(t, 1, 4)
	count := func(kind, id string) int {
		t.Helper()
		var n int
		if err := w.DB().QueryRow(`SELECT count(*) FROM resource_access_edges WHERE source_kind=? AND source_id=?`, kind, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("document_revision", "rev-1") == 0 || count("artifact", "art-1") == 0 {
		t.Fatal("fixture lacks revision or artifact edges")
	}
	run := func(stop func() bool) bool {
		t.Helper()
		tx, err := w.DB().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		done, err := moveDocumentAccessHistoryUntil(context.Background(), tx, 1000, stop)
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return done
	}
	run(func() bool { return true })
	var cursor string
	if err := w.DB().QueryRow(`SELECT cursor FROM resource_access_document_history_job`).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if count("document_revision", "rev-1") != 0 || count("artifact", "art-1") == 0 || cursor != "" {
		t.Fatalf("over-budget chunk: revision=%d artifact=%d cursor=%q", count("document_revision", "rev-1"), count("artifact", "art-1"), cursor)
	}
	if !run(func() bool { return false }) || count("artifact", "art-1") != 0 {
		t.Fatal("next chunk did not finish the revision")
	}
}

// Chunks used to run under a 100ms context deadline. On a slow or large
// database every chunk could overrun it, so none committed, and an overrun
// that cancelled a statement mid-transaction could leave the write lock held.
// The budget is now checked between statements: even a chunk that is already
// over budget commits one statement pair, never cancels SQL, and leaves the
// database writable.
func TestDocumentAccessHistoryChunkProgressesWithoutCancellingSQL(t *testing.T) {
	t.Parallel()
	w := legacyDocumentHistoryWorkspace(t, 4, 60)
	other, err := sql.Open("sqlite", sqliteDSN(w.Layout().DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	writable := func() error {
		c, err := other.Conn(context.Background())
		if err != nil {
			return err
		}
		defer c.Close()
		if _, err = c.ExecContext(context.Background(), `PRAGMA busy_timeout=500`); err != nil {
			return err
		}
		if _, err = c.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
			return err
		}
		_, err = c.ExecContext(context.Background(), `ROLLBACK`)
		return err
	}
	remaining := perRevisionEdgeCount(t, w)
	for chunks := 0; ; chunks++ {
		if chunks > 200 {
			t.Fatal("backfill did not converge")
		}
		// An already-expired caller deadline and a zero budget: the worst case.
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		<-ctx.Done()
		done, err := w.maintainDocumentAccessHistoryChunk(ctx, 1000, 0)
		cancel()
		if err != nil {
			t.Fatalf("chunk %d: %v", chunks, err)
		}
		if err = writable(); err != nil {
			t.Fatalf("chunk %d left the database locked: %v", chunks, err)
		}
		left := perRevisionEdgeCount(t, w)
		if !done && left >= remaining && left > 0 {
			t.Fatalf("chunk %d made no progress (%d edges left)", chunks, left)
		}
		if remaining-left > documentHistoryEdgeBatch*2 {
			t.Fatalf("over-budget chunk moved %d edges", remaining-left)
		}
		remaining = left
		if done {
			break
		}
	}
	if remaining != 0 {
		t.Fatalf("%d per-revision edges left", remaining)
	}
}
