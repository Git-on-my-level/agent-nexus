package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/scopesearch"
	"agent-nexus-core/internal/testutil/perfguard"
)

func commentSearchExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func commentSearchEvent(t *testing.T, db *sql.DB, id, text string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"payload": map[string]any{"text": text}})
	commentSearchExec(t, db, `INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'message_posted','now','owner','thread','[]',?)`, id, string(body))
}
func commentSearchCount(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCommentSearchCanonicalImportsLifecycleAndRollback(t *testing.T) {
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	commentSearchEvent(t, db, "comment", "old token")
	for _, q := range []string{
		`UPDATE events SET payload_json='{"payload":{"text":"edited needle"}}' WHERE id='comment'`,
		`UPDATE events SET refs_json='["card:private"]' WHERE id='comment'`,
		`UPDATE events SET thread_id='new-thread' WHERE id='comment'`,
		`UPDATE events SET archived_at='now' WHERE id='comment'`,
	} {
		commentSearchExec(t, db, q)
	}
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_postings WHERE thread_id='new-thread' AND term='needle'`); n != 1 {
		t.Fatal(n)
	}
	if n := commentSearchCount(t, db, `SELECT source_version FROM comment_search_heads WHERE event_id='comment'`); n != 5 {
		t.Fatal(n)
	}
	commentSearchExec(t, db, `UPDATE events SET trashed_at='now' WHERE id='comment'`)
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_heads`); n != 0 {
		t.Fatal(n)
	}
	commentSearchExec(t, db, `UPDATE events SET trashed_at=NULL WHERE id='comment'`)
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_postings`); n != 2 {
		t.Fatal(n)
	}
	// A failure after deleting old postings must roll back the canonical edit,
	// original terms and version in the same source transaction.
	commentSearchExec(t, db, `CREATE TRIGGER reject_comment_search BEFORE INSERT ON comment_search_postings BEGIN SELECT RAISE(ABORT,'injected index failure'); END`)
	if _, err = db.Exec(`UPDATE events SET payload_json='{"payload":{"text":"replacement"}}' WHERE id='comment'`); err == nil {
		t.Fatal("source edit survived index failure")
	}
	if n := commentSearchCount(t, db, `SELECT count(*) FROM events WHERE id='comment' AND json_extract(payload_json,'$.payload.text')='edited needle'`); n != 1 {
		t.Fatal(n)
	}
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_postings WHERE term='needle'`); n != 1 {
		t.Fatal(n)
	}
	commentSearchExec(t, db, `DROP TRIGGER reject_comment_search`)
	commentSearchExec(t, db, `INSERT OR REPLACE INTO events(id,type,ts,actor_id,thread_id,payload_json) VALUES('comment','other','now','owner','thread','{}')`)
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_heads`); n != 0 {
		t.Fatal("replace left stale head", n)
	}
	commentSearchExec(t, db, `UPDATE events SET type='message_posted',payload_json='{}' WHERE id='comment'`)
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_heads`); n != 1 {
		t.Fatal("empty comment missing", n)
	}
	commentSearchExec(t, db, `DELETE FROM events WHERE id='comment'`)
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_heads`); n != 0 {
		t.Fatal(n)
	}
}

func TestCommentSearchOnlineBackfillResumesAndCertifiesSourceBindings(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ws.Close() }()
	// Reinstall the metadata-only migration over a legacy corpus. Installation
	// must not parse payloads or copy history before the listener can start.
	for _, q := range []string{`DROP TRIGGER comment_search_event_insert`, `DROP TRIGGER comment_search_event_update`, `DROP TRIGGER comment_search_event_delete`, `DROP TABLE comment_search_postings`, `DROP TABLE comment_search_heads`, `DROP TABLE comment_search_job`} {
		commentSearchExec(t, ws.DB(), q)
	}
	for i := 0; i < 20; i++ {
		commentSearchEvent(t, ws.DB(), fmt.Sprintf("row-%03d", i), "historical token")
	}
	commentSearchEvent(t, ws.DB(), "", "empty identity")
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installCommentSearch(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_heads`); n != 0 {
		t.Fatal("migration backfilled", n)
	}
	for i := 0; i < 4; i++ {
		if done, err := ws.MaintainCommentSearchBatch(ctx); err != nil || done {
			t.Fatal(done, err)
		}
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_heads`); n != 4 {
		t.Fatal("unbounded chunk", n)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	commentSearchEvent(t, ws.DB(), "a-behind-cursor", "concurrent insert")
	commentSearchExec(t, ws.DB(), `UPDATE events SET payload_json='{"payload":{"text":"concurrent edit"}}' WHERE id='row-000'`)
	commentSearchExec(t, ws.DB(), `DELETE FROM events WHERE id='row-001'`)
	commentSearchExec(t, ws.DB(), `UPDATE events SET trashed_at='now' WHERE id='row-015'`)
	commentSearchExec(t, ws.DB(), `UPDATE events SET thread_id='private-thread' WHERE id='row-010'`)
	for i := 0; ; i++ {
		if i > 30 {
			t.Fatal("worker did not converge")
		}
		done, err := ws.MaintainCommentSearchBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM events e LEFT JOIN comment_search_heads h ON h.event_id=e.id WHERE e.type='message_posted' AND COALESCE(e.thread_id,'')<>'' AND COALESCE(trim(e.trashed_at),'')='' AND (h.event_id IS NULL OR h.thread_id<>e.thread_id OR h.projection_json<>anx_comment_search_projection(CAST(json_extract(e.payload_json,'$.payload.text') AS BLOB)) OR h.authority_binding<>'canonical-event-v1')`); n != 0 {
		t.Fatal("uncertified source", n)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_heads h LEFT JOIN events e ON e.id=h.event_id WHERE e.id IS NULL OR COALESCE(trim(e.trashed_at),'')<>''`); n != 0 {
		t.Fatal("stale row", n)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT done FROM comment_search_job WHERE authority_binding='canonical-event-v1'`); n != 1 {
		t.Fatal("uncertified generation", n)
	}
	// Certification stays valid through new canonical writes; the source
	// authorization graph, not a copied audience list, controls subsequent reads.
	commentSearchEvent(t, ws.DB(), "after-done", "live token")
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_postings WHERE event_id='after-done'`); n != 2 {
		t.Fatal(n)
	}
}

func TestCommentSearchBackfillFailureCancellationAndContention(t *testing.T) {
	ctx := context.Background()
	ws, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	commentSearchEvent(t, ws.DB(), "event", "needle")
	commentSearchExec(t, ws.DB(), `UPDATE comment_search_job SET done=0,cursor=NULL`)
	commentSearchExec(t, ws.DB(), `CREATE TRIGGER reject_comment_backfill BEFORE UPDATE ON comment_search_heads BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
	if _, err = ws.MaintainCommentSearchBatch(ctx); err == nil {
		t.Fatal("failure ignored")
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_job WHERE cursor IS NULL AND done=0`); n != 1 {
		t.Fatal("failure advanced watermark")
	}
	commentSearchExec(t, ws.DB(), `DROP TRIGGER reject_comment_backfill`)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = ws.MaintainCommentSearchBatch(cancelled); err == nil {
		t.Fatal("cancellation ignored")
	}
	held, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	started := time.Now()
	_, err = ws.MaintainCommentSearchBatch(ctx)
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("contention did not yield", err, time.Since(started))
	}
	held.Rollback()
	if n := commentSearchCount(t, ws.DB(), `PRAGMA busy_timeout`); n != 20000 {
		t.Fatal("pool timeout leaked", n)
	}
	if _, err = ws.MaintainCommentSearchBatch(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCommentSearchCapsAndResourceDeletePlan(t *testing.T) {
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	words := make([]string, scopesearch.MaxTerms+1)
	for i := range words {
		words[i] = fmt.Sprintf("term%d", i)
	}
	commentSearchEvent(t, ws.DB(), "max", strings.Join(words, " "))
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_postings WHERE event_id='max'`); n != 4096 {
		t.Fatal(n)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT json_extract(projection_json,'$.truncated') FROM comment_search_heads WHERE event_id='max'`); n != 1 {
		t.Fatal(n)
	}
	if _, err = ws.DB().Exec(`INSERT INTO comment_search_postings VALUES('thread','overflow','max',4096)`); err == nil {
		t.Fatal("posting overflow accepted")
	}
	rows, err := ws.DB().Query(`EXPLAIN QUERY PLAN DELETE FROM comment_search_postings WHERE event_id=?`, "max")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "SEARCH comment_search_postings USING PRIMARY KEY (event_id=?)") {
			t.Fatal(detail)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestPerformanceCommentSearchWriteHistoryIndependence(t *testing.T) {
	// Isolate the real event-trigger writer from the unchanged legacy graph.
	// This is storage work, not a claim that the full comment API has cut over.
	db, capture, err := perfguard.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	commentSearchExec(t, db, `CREATE TABLE events(id TEXT PRIMARY KEY,type TEXT,ts TEXT,actor_id TEXT,thread_id TEXT,refs_json TEXT,payload_json TEXT,trashed_at TEXT,archived_at TEXT)`)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installCommentSearch(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	commentSearchEvent(t, db, "target", "old token")
	prior := 0
	var initial uint64
	for _, n := range []int{100, 1000} {
		for i := prior; i < n; i++ {
			commentSearchEvent(t, db, fmt.Sprintf("peer-%04d", i), "peer history")
		}
		prior = n
		capture.Start()
		commentSearchExec(t, db, `UPDATE events SET payload_json='{"payload":{"text":"new token"}}' WHERE id='target'`)
		_, queries, rows := capture.Stop()
		if err = capture.WorkError(); err != nil {
			t.Fatal(err)
		}
		work := capture.Work()
		t.Logf("comments=%d canonical_update_sql=%d rows=%d vm_steps=%d full_scan=%d", n, queries, rows, work.VMSteps, work.FullScanSteps)
		if work.FullScanSteps != 0 || work.Sorts != 0 || work.AutoIndexRows != 0 || work.VMSteps > 2000 {
			t.Fatal(work)
		}
		if initial == 0 {
			initial = work.VMSteps
		} else if work.VMSteps > initial+100 {
			t.Fatal("write grew with history", initial, work)
		}
	}
}
