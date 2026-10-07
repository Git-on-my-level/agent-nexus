package primitives

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestEventStreamPagesAdvanceAcrossHiddenRowsAndFreshAuthority(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
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
	for _, id := range []string{"", "unknown"} {
		cur, err := s.EventStreamCursor(scope, id)
		if err != nil || cur.ID != "0450" {
			t.Fatalf("head: %+v %v", cur, err)
		}
	}
}

func TestEventStreamChronologyAndSparseFilters(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
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

func TestEventStreamBatchesRevisionRefs(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
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
	if len(page.Events) != 200 || counter.Count() != 3 || counter.ReturnedRows() != 202 {
		t.Fatalf("revision N+1: events=%d reads=%d rows=%d", len(page.Events), counter.Count(), counter.ReturnedRows())
	}
	for _, e := range page.Events {
		if e["payload"].(map[string]any)["revision_ref"] != "document_revision:doc-handle-r1" {
			t.Fatalf("revision presentation: %+v", e)
		}
	}
}
