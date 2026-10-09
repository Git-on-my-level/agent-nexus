package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/testsql"
)

func TestEventStreamPagesAdvanceAcrossHiddenRowsAndFreshAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('private','now','owner','{"pm_actor_id":"owner"}'),('revoked','now','owner','{}')`,
		`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('0000','message_posted','2026-01-01T00:00:00Z','owner','[]','{}')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 450; i++ {
		thread := "private"
		if i > 200 {
			thread = "revoked"
		}
		if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'message_posted','2026-01-01T00:00:00Z','owner',?,'[]','{}')`, fmt.Sprintf("%04d", i), thread); err != nil {
			t.Fatal(err)
		}
	}
	scope := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	cursor, err := s.EventStreamCursor(scope, "0000")
	if err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	first, err := s.ListEventStreamPage(scope, EventListFilter{}, cursor)
	if err != nil || len(first.Events) != 0 || first.Cursor.ID != "0200" || !first.HasMore {
		t.Fatalf("hidden page: %+v %v", first, err)
	}
	if counter.Count() > 4 || counter.ReturnedRows() > 203 {
		t.Fatalf("hidden page read unbounded: %d statements %d rows", counter.Count(), counter.ReturnedRows())
	}
	second, err := s.ListEventStreamPage(scope, EventListFilter{}, first.Cursor)
	if err != nil || len(second.Events) != 200 || second.Events[0]["id"] != "0201" || second.Events[199]["id"] != "0400" {
		t.Fatalf("second page: %+v %v", second, err)
	}
	// Reuse the same stream scope: a permission change must invalidate shared
	// denial snapshots before the next tick, even for already known resources.
	if _, err := db.Exec(`UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='revoked'`); err != nil {
		t.Fatal(err)
	}
	third, err := s.ListEventStreamPage(scope, EventListFilter{}, second.Cursor)
	if err != nil || len(third.Events) != 0 || third.Cursor.ID != "0450" || third.HasMore {
		t.Fatalf("revoked page: %+v %v", third, err)
	}
	fourth, err := s.ListEventStreamPage(scope, EventListFilter{}, third.Cursor)
	if err != nil || len(fourth.Events) != 0 || fourth.Cursor != third.Cursor {
		t.Fatalf("idle page: %+v %v", fourth, err)
	}
	// Owner resume crosses page boundaries without loss or duplicate rows.
	owner := WithAccessScope(ctx, AccessScope{ActorID: "owner"})
	seen := map[string]bool{}
	for cur := cursor; ; {
		page, err := s.ListEventStreamPage(owner, EventListFilter{}, cur)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Events {
			id := e["id"].(string)
			if seen[id] {
				t.Fatal("duplicate " + id)
			}
			seen[id] = true
		}
		cur = page.Cursor
		if !page.HasMore {
			break
		}
	}
	if len(seen) != 450 {
		t.Fatalf("lost rows: %d", len(seen))
	}
	// Both empty and unknown initial resume IDs seed head, never replay.
	for _, id := range []string{"", "unknown", "0001", "0450"} {
		cur, err := s.EventStreamCursor(scope, id)
		if err != nil || cur.ID != "0450" {
			t.Fatalf("head: %+v %v", cur, err)
		}
	}
}

func TestEventStreamChronologyAndSparseFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, row := range [][3]string{
		{"z", "2026-01-01T00:00:00Z", "other"},
		{"a", "2026-01-01T00:00:00.1Z", "wanted"},
		{"b", "2026-01-01T01:00:00.100000000+01:00", "wanted"},
		{"c", "2026-01-01T00:00:01Z", "wanted"},
	} {
		if _, err := ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'message_posted',?,'actor',?,'[]','{}')`, row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListEventStreamPage(ctx, EventListFilter{ThreadIDs: []string{"wanted"}, Types: []string{"message_posted"}}, EventCursor{TS: "2026-01-01T00:00:00Z", ID: "z"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range page.Events {
		ids = append(ids, e["id"].(string))
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("chronology: %v", ids)
	}
	page, err = s.ListEventStreamPage(ctx, EventListFilter{ThreadID: "absent"}, EventCursor{})
	if err != nil || len(page.Events) != 0 || page.Cursor.ID != "c" {
		t.Fatalf("filter stalled: %+v %v", page, err)
	}
}

func TestEventStreamPlainThreadAppendRefreshesInheritedDenials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	if _, err := ws.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('private','now','owner','{"pm_actor_id":"owner"}')`); err != nil {
		t.Fatal(err)
	}
	scope := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	if _, err := ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('0001','message_posted','2026-01-01T00:00:00Z','owner','private','[]','{"text":"plain message"}')`); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListEventStreamPage(scope, EventListFilter{}, EventCursor{})
	if err != nil || len(first.Events) != 0 {
		t.Fatalf("prime: %+v %v", first, err)
	}
	var before, after int64
	if err := ws.DB().QueryRow(`SELECT version FROM resource_access_epoch`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('0002','message_posted','2026-01-01T00:00:00Z','owner','private','[]','{"text":"another plain message"}')`); err != nil {
		t.Fatal(err)
	}
	if err := ws.DB().QueryRow(`SELECT version FROM resource_access_epoch`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListEventStreamPage(scope, EventListFilter{}, first.Cursor)
	if err != nil || len(page.Events) != 0 || page.Cursor.ID != "0002" || after <= before {
		t.Fatalf("plain append needs fresh inherited denial: %+v epochs=%d/%d err=%v", page, before, after, err)
	}
}

func TestEventStreamBatchesRevisionRefs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, q := range []string{
		`INSERT INTO documents(id,handle,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES('doc','doc-handle','Doc','dr',1,'now','actor','now','actor')`,
		`INSERT INTO cards(id,handle,title,created_at,created_by,updated_at,updated_by) VALUES('card','card-handle','Card','now','actor','now','actor')`,
		`INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,created_at,created_by) VALUES('dr','doc',1,'blob','now','actor')`,
		`INSERT INTO card_revisions(revision_id,card_id,revision_number,artifact_id,created_at,created_by) VALUES('cr','card',1,'blob','now','actor')`,
	} {
		if _, err := ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 200; i++ {
		if _, err := ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES(?,'message_posted','2026-01-01T00:00:00Z','actor','["document_revision:dr","card_revision:cr","document_revision:missing"]','{"payload":{"revision_ref":"document_revision:dr"}}')`, fmt.Sprintf("%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	page, err := s.ListEventStreamPage(ctx, EventListFilter{}, EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 200 || counter.Count() != 4 || counter.ReturnedRows() != 402 {
		t.Fatalf("revision N+1: events=%d reads=%d rows=%d", len(page.Events), counter.Count(), counter.ReturnedRows())
	}
	for _, e := range page.Events {
		if e["payload"].(map[string]any)["revision_ref"] != "document_revision:doc-handle-r1" {
			t.Fatalf("revision presentation: %+v", e)
		}
	}
	// Inspect the actual scoped statements, including relation rewriting and
	// narrowed denial bindings, rather than only the metadata traversal plan.
	counter.Reset()
	if _, err := s.ListEventStreamPage(WithAccessScope(ctx, AccessScope{ActorID: "reader"}), EventListFilter{}, EventCursor{}); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, statement := range counter.Statements() {
		if !strings.Contains(statement.SQL, "SELECT kind,id FROM _anx_fresh_denied UNION SELECT") {
			continue
		}
		rows, err := db.Query("EXPLAIN QUERY PLAN "+statement.SQL, statement.Args...)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var a, b, c int
			var detail string
			if err := rows.Scan(&a, &b, &c, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(details, "; ")
		var expected string
		switch {
		case strings.Contains(statement.SQL, "FROM events e WHERE"):
			expected = "SEARCH _row USING INDEX sqlite_autoindex_events_1 (id=?)"
		case strings.Contains(statement.SQL, "FROM card_revisions r JOIN"):
			expected = "SEARCH _row USING INDEX sqlite_autoindex_card_revisions_1 (revision_id=?)"
		case strings.Contains(statement.SQL, "FROM document_revisions r LEFT JOIN"):
			expected = "SEARCH _row USING INDEX sqlite_autoindex_document_revisions_1 (revision_id=?)"
		}
		if expected == "" || !strings.Contains(plan, expected) || strings.Contains(plan, "SCAN _row") {
			t.Fatalf("scoped page lost primary-key lookups: %s", plan)
		}
		var lookups []string
		for _, detail := range details {
			if strings.Contains(detail, "SEARCH _row") {
				lookups = append(lookups, detail)
			}
		}
		t.Logf("scoped event page lookups: %s", strings.Join(lookups, "; "))
		checked++
	}
	if checked != 3 {
		t.Fatalf("scoped payload and two revision batches: got %d statements", checked)
	}
}

func TestEventStreamWarmHiddenPageCostIsIndependentOfDenialHistory(t *testing.T) {
	t.Parallel()
	for _, total := range []int{1000, 10000} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if _, err := ws.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('private','now','owner','{"pm_actor_id":"owner"}')`); err != nil {
				t.Fatal(err)
			}
			if _, err := ws.DB().Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<?)
				INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json)
				SELECT printf('%06d',i),'message_posted','2026-01-01T00:00:00Z','owner','private','[]','{}' FROM n`, total); err != nil {
				t.Fatal(err)
			}
			db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
			defer db.Close()
			s := NewTestStore(db, ws.Layout().ArtifactContentDir)
			scope := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
			// Cold capture builds the canonical closure and its shared lookup once.
			if _, err := s.ListEventStreamPage(scope, EventListFilter{}, EventCursor{}); err != nil {
				t.Fatal(err)
			}
			cursor := EventCursor{}
			for tick := 0; tick < 3; tick++ {
				counter.Reset()
				start := time.Now()
				page, err := s.ListEventStreamPage(scope, EventListFilter{}, cursor)
				elapsed := time.Since(start)
				if err != nil || len(page.Events) != 0 || !page.HasMore || page.Cursor.ID != fmt.Sprintf("%06d", (tick+1)*200) {
					t.Fatalf("hidden traversal: %+v %v", page, err)
				}
				if counter.Count() != 3 || counter.ReturnedRows() != 202 || elapsed > 500*time.Millisecond {
					t.Fatalf("hidden tick: %s statements=%d rows=%d", elapsed, counter.Count(), counter.ReturnedRows())
				}
				bound := false
				for _, statement := range counter.Statements() {
					if !strings.Contains(statement.SQL, "SELECT kind,id FROM _anx_fresh_denied UNION SELECT") {
						continue
					}
					var denied [][2]string
					if err := json.Unmarshal([]byte(statement.Args[0].(string)), &denied); err != nil || len(denied) != 200 {
						t.Fatalf("denial binding includes history: %d %v", len(denied), err)
					}
					bound = true
				}
				if !bound {
					t.Fatal("did not exercise narrowed cached denial binding")
				}
				t.Logf("hidden=%d tick=%d elapsed=%s statements=%d rows=%d denied_bindings=200", total, tick, elapsed, counter.Count(), counter.ReturnedRows())
				cursor = page.Cursor
			}
		})
	}
}

func TestEventStreamNarrowedSnapshotFallsBackOnConcurrentParentRevocation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "doc", "title": "source"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	if err := ws.DB().QueryRow(`SELECT revision_id FROM document_revisions WHERE document_id='doc'`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('event','message_posted','2026-01-01T00:00:00Z','owner',?,'[]','{}')`, doc["thread_id"]); err != nil {
		t.Fatal(err)
	}
	pageCtx := withEventPageAccessScope(WithAccessScope(ctx, AccessScope{ActorID: "reader"}), []string{"event"})
	refCtx := eventPageRefScope(pageCtx, "document_revision", []string{revision})
	db := resourceaccess.NewDB(ws.DB())
	assertVisible := func(want int) {
		t.Helper()
		var events, revisions int
		if err := db.QueryRowContext(pageCtx, `SELECT count(*) FROM events WHERE id='event'`).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(refCtx, `SELECT count(*) FROM document_revisions r JOIN documents d ON d.id=r.document_id WHERE r.revision_id=?`, revision).Scan(&revisions); err != nil {
			t.Fatal(err)
		}
		if events != want || revisions != want {
			t.Fatalf("events=%d revisions=%d want=%d", events, revisions, want)
		}
	}
	assertVisible(1)
	first := denialSnapshotFrom(pageCtx)
	for _, owner := range []string{"owner", "", "owner"} {
		if _, err := s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
		want := 0
		if owner == "" {
			want = 1
		}
		assertVisible(want)
		if denialSnapshotFrom(pageCtx) != first {
			t.Fatal("expected SQL epoch fallback on the existing page snapshot")
		}
		// A fresh page must also honor inherited event/revision denial keys.
		pageCtx = withEventPageAccessScope(WithAccessScope(ctx, AccessScope{ActorID: "reader"}), []string{"event"})
		refCtx = eventPageRefScope(pageCtx, "document_revision", []string{revision})
		assertVisible(want)
		first = denialSnapshotFrom(pageCtx)
	}
}
