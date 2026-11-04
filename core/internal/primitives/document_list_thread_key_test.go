package primitives

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
)

// legacyDocumentListAggregates is the document list aggregate SQL as it stood
// before migration 131. It is kept here only to prove the indexed form returns
// the same rows.
const legacyDocumentListAggregates = `
SELECT dp.*,
	COALESCE(rc.revision_cnt, 0),
	COALESCE(tmc.timeline_msg_cnt, 0),
	tml.last_body, tml.last_at, tml.last_by
FROM doc_page dp
LEFT JOIN (
	SELECT dr2.document_id, COUNT(*) AS revision_cnt FROM document_revisions dr2
	WHERE dr2.document_id IN (SELECT id FROM doc_page)
	GROUP BY dr2.document_id
) rc ON dp.id = rc.document_id
LEFT JOIN (
	SELECT trim(COALESCE(e.thread_id,'')) AS tid, COUNT(*) AS timeline_msg_cnt FROM events e
	WHERE e.type = 'message_posted'
	  AND COALESCE(trim(e.thread_id),'') <> ''
	  AND COALESCE(trim(e.trashed_at),'') = ''
	  AND trim(COALESCE(e.thread_id,'')) IN (
		SELECT DISTINCT trim(COALESCE(thread_id,'')) FROM doc_page WHERE COALESCE(trim(thread_id),'') <> ''
	  )
	GROUP BY tid
) tmc ON trim(COALESCE(dp.thread_id,'')) = tmc.tid
LEFT JOIN (
	SELECT tid, last_body, last_at, last_by FROM (
		SELECT trim(COALESCE(e.thread_id,'')) AS tid,
			TRIM(COALESCE(NULLIF(json_extract(e.payload_json, '$.text'), ''), json_extract(e.payload_json, '$.summary'))) AS last_body,
			e.ts AS last_at, e.actor_id AS last_by,
			ROW_NUMBER() OVER (PARTITION BY trim(COALESCE(e.thread_id,'')) ORDER BY e.ts DESC, e.id DESC) AS rn
		FROM events e
		WHERE e.type = 'message_posted'
		  AND COALESCE(trim(e.thread_id),'') <> ''
		  AND COALESCE(trim(e.trashed_at),'') = ''
		  AND trim(COALESCE(e.thread_id,'')) IN (
			SELECT DISTINCT trim(COALESCE(thread_id,'')) FROM doc_page WHERE COALESCE(trim(thread_id),'') <> ''
		  )
	) ranked WHERE rn = 1
) tml ON trim(COALESCE(dp.thread_id,'')) = tml.tid`

func legacyListDocumentsQuery(t *testing.T, filter DocumentListFilter) (string, []any) {
	t.Helper()
	filter.metadataOnly = true
	metadata, args := buildListDocumentsQuery(filter)
	const suffix = ` SELECT dp.*,NULL,NULL,NULL,NULL,NULL FROM doc_page dp`
	if !strings.HasPrefix(metadata, `WITH doc_page AS (`) || !strings.HasSuffix(metadata, suffix) {
		t.Fatalf("unexpected metadata query shape:\n%s", metadata)
	}
	return strings.TrimSuffix(metadata, suffix) + legacyDocumentListAggregates, args
}

func documentListRows(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any) [][]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query: %v\n%s", err, query)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		row := make([]string, len(values))
		for i, v := range values {
			row[i] = fmt.Sprintf("%T:%v", v, v)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func seedDocumentListThreadKeys(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	// Documents: normal, space-padded, NULL and blank thread ids.
	docs := []struct {
		id     string
		thread any
		at     string
	}{
		{"doc-normal", "thread-normal", "2026-01-01T00:00:05Z"},
		{"doc-padded", "  thread-padded ", "2026-01-01T00:00:04Z"},
		{"doc-null", nil, "2026-01-01T00:00:03Z"},
		{"doc-blank", "   ", "2026-01-01T00:00:02Z"},
		{"doc-quiet", "thread-quiet", "2026-01-01T00:00:01Z"},
	}
	for _, d := range docs {
		exec(`INSERT INTO documents(id,handle,thread_id,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,?,'',0,?,'owner',?,'owner')`, d.id, d.id, d.thread, "Title "+d.id, d.at, d.at)
	}
	events := []struct {
		id, typ  string
		thread   any
		ts       string
		text     string
		trashed  any
		payloadF string
	}{
		{"ev-1", "message_posted", "thread-normal", "2026-02-01T00:00:01Z", "first", nil, "text"},
		{"ev-2", "message_posted", " thread-normal ", "2026-02-01T00:00:03Z", "padded writer", nil, "text"},
		{"ev-3", "message_posted", "thread-normal", "2026-02-01T00:00:02Z", "", nil, "summary"},
		{"ev-4", "message_posted", "thread-normal", "2026-02-01T00:00:09Z", "trashed latest", "2026-02-02T00:00:00Z", "text"},
		{"ev-5", "message_posted", "thread-normal", "2026-02-01T00:00:08Z", "blank trash", "   ", "text"},
		{"ev-6", "card_moved", "thread-normal", "2026-02-01T00:00:10Z", "other type", nil, "text"},
		{"ev-7", "message_posted", "thread-padded", "2026-02-01T00:00:01Z", "unpadded writer", nil, "text"},
		{"ev-8", "message_posted", "   thread-padded", "2026-02-01T00:00:01Z", "same ts tie", nil, "text"},
		{"ev-9", "message_posted", nil, "2026-02-01T00:00:01Z", "threadless", nil, "text"},
		{"ev-10", "message_posted", "", "2026-02-01T00:00:01Z", "blank thread", nil, "text"},
		{"ev-11", "message_posted", "  ", "2026-02-01T00:00:01Z", "space thread", nil, "text"},
		{"ev-12", "message_posted", "thread-elsewhere", "2026-02-01T00:00:01Z", "unlisted thread", nil, "text"},
	}
	for _, e := range events {
		payload := fmt.Sprintf(`{"%s":%q}`, e.payloadF, e.text)
		exec(`INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json,trashed_at) VALUES(?,?,?,?,'owner',?,'[]',?,?)`, e.id, e.id, e.typ, e.ts, e.thread, payload, e.trashed)
	}
}

// TestDocumentListThreadKeyIndexMatchesLegacyQuery runs the pre-131 aggregate
// and the indexed aggregate on one seeded database and requires identical rows
// for NULL, blank, space-padded and normal thread ids on both sides of the
// join, then requires the indexed plan to seek events by the thread key.
func TestDocumentListThreadKeyIndexMatchesLegacyQuery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	db := ws.DB()
	seedDocumentListThreadKeys(t, ctx, db)

	limit := 2
	filters := map[string]DocumentListFilter{
		"all":    {},
		"page":   {Limit: &limit},
		"page 2": {Limit: &limit, Cursor: encodeCursor(2)},
		"thread": {ThreadID: "thread-normal"},
	}
	for name, filter := range filters {
		filter.States = NormalizeListLifecycleStates(filter.States)
		legacy, legacyArgs := legacyListDocumentsQuery(t, filter)
		current, args := buildListDocumentsQuery(filter)
		want := documentListRows(t, ctx, db, legacy, legacyArgs)
		got := documentListRows(t, ctx, db, current, args)
		if len(want) == 0 {
			t.Fatalf("%s: seeded query returned no rows", name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: indexed document list differs from legacy\n got: %v\nwant: %v", name, got, want)
		}
	}

	// Pin the aggregate values the comparison covers, so an empty join on
	// both sides cannot pass vacuously.
	query, args := buildListDocumentsQuery(DocumentListFilter{States: NormalizeListLifecycleStates(nil)})
	rows := documentListRows(t, ctx, db, query, args)
	summary := map[string][2]string{}
	for _, row := range rows {
		n := len(row)
		summary[row[0]] = [2]string{row[n-4], row[n-3]}
	}
	wantSummary := map[string][2]string{
		"string:doc-normal": {"int64:4", "string:blank trash"},
		"string:doc-padded": {"int64:2", "string:same ts tie"},
		"string:doc-null":   {"int64:0", "<nil>:<nil>"},
		"string:doc-blank":  {"int64:0", "<nil>:<nil>"},
		"string:doc-quiet":  {"int64:0", "<nil>:<nil>"},
	}
	if !reflect.DeepEqual(summary, wantSummary) {
		t.Fatalf("aggregate summary = %v, want %v", summary, wantSummary)
	}

	plan := explainQueryPlan(t, db, query, args...)
	if !strings.Contains(plan, "idx_events_message_thread_key") {
		t.Fatalf("document list aggregates do not use the thread key index:\n%s", plan)
	}
	for _, line := range strings.Split(plan, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "SCAN e") {
			t.Fatalf("document list aggregates scan events:\n%s", plan)
		}
	}
}

// TestDocumentListThreadKeyMigrationIsIdempotent rewinds migration 131 and
// reopens the workspace twice: the index is rebuilt, stored thread ids are
// untouched, and reapplying over an existing index is a no-op.
func TestDocumentListThreadKeyMigrationIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	seedDocumentListThreadKeys(t, ctx, ws.DB())
	snapshot := func(db *sql.DB) []string {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT id, quote(thread_id) FROM events ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id, thread string
			if err := rows.Scan(&id, &thread); err != nil {
				t.Fatal(err)
			}
			out = append(out, id+"="+thread)
		}
		return out
	}
	indexCount := func(db *sql.DB) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='index' AND name='idx_events_message_thread_key'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := snapshot(ws.DB())
	if indexCount(ws.DB()) != 1 {
		t.Fatal("fresh workspace is missing idx_events_message_thread_key")
	}
	for _, rewind := range []string{
		`DROP INDEX idx_events_message_thread_key`, // interrupted before the index
		``, // index present, version unrecorded
	} {
		if rewind != "" {
			if _, err := ws.DB().ExecContext(ctx, rewind); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ws.DB().ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 131`); err != nil {
			t.Fatal(err)
		}
		if err := ws.Close(); err != nil {
			t.Fatal(err)
		}
		ws, err = storage.InitializeWorkspace(ctx, root)
		if err != nil {
			t.Fatalf("reopen workspace (runs migration 131): %v", err)
		}
		if indexCount(ws.DB()) != 1 {
			t.Fatal("migration 131 did not install idx_events_message_thread_key")
		}
		if after := snapshot(ws.DB()); !reflect.DeepEqual(after, before) {
			t.Fatalf("migration 131 changed stored thread ids\n got: %v\nwant: %v", after, before)
		}
	}
	_ = ws.Close()
}
