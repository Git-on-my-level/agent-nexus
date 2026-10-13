package primitives

import (
	"context"
	"strings"
	"testing"
)

func TestCommentSearchExternalWriteGateAndTransactionalFailure(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "external"}, "plan", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := doc["id"].(string)
	thread := doc["thread_id"].(string)
	if _, err = ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('external','message_posted','now','owner',?,'[]','{"payload":{"text":"café approved"}}')`, thread); err != nil {
		t.Fatal(err)
	}
	coverage := DocumentSearchCoverage{}
	got, _, err := s.SearchDocuments(ctx, DocumentSearchFilter{Query: "plan cafe", Coverage: &coverage})
	if err != nil || len(got) != 1 || coverage.Mode != "aggregate" {
		t.Fatal("pending source served independently or fallback stale", got, coverage, err)
	}
	for done := false; !done; {
		done, err = ws.MaintainCommentSearchBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _, err = s.SearchDocuments(ctx, DocumentSearchFilter{Query: "plan cafe", Coverage: &coverage})
	if err != nil || len(got) != 1 || coverage.Mode != "independent" {
		t.Fatal(got, coverage, err)
	}
	if _, err = ws.DB().Exec(`CREATE TRIGGER reject_normalization BEFORE INSERT ON comment_search_postings BEGIN SELECT RAISE(ABORT,'injected projection failure');END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateDocumentComment(ctx, "owner", id, "external", "replacement"); err == nil {
		t.Fatal("core source committed despite normalization failure")
	}
	var text string
	if err = ws.DB().QueryRow(`SELECT json_extract(payload_json,'$.payload.text') FROM events WHERE id='external'`).Scan(&text); err != nil || text != "café approved" {
		t.Fatal("source transaction not rolled back", text, err)
	}
	if _, err = ws.DB().Exec(`DROP TRIGGER reject_normalization`); err != nil {
		t.Fatal(err)
	}
	// A changed source's current private references constrain fallback and index
	// immediately, even before the asynchronous normalization worker runs.
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE events SET refs_json=? WHERE id='external'`, `["`+private["ref"].(string)+`"]`); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"reader", "unauthorized-agent"} {
		got, next, e := s.SearchDocuments(WithRequestAccessScope(ctx, AccessScope{ActorID: actor}), DocumentSearchFilter{Query: "plan cafe"})
		if e != nil || len(got) != 0 || next != "" {
			t.Fatal("pending private source leaked", got, next, e)
		}
	}
	for done := false; !done; {
		done, err = ws.MaintainCommentSearchBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _, err = s.SearchDocuments(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), DocumentSearchFilter{Query: "plan cafe"})
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	var triggers string
	rows, err := ws.DB().Query(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name LIKE 'document_search_%'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var q string
		if err = rows.Scan(&q); err != nil {
			t.Fatal(err)
		}
		triggers += q
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if strings.Contains(triggers, "anx_") {
		t.Fatal("document search trigger requires application function")
	}
}
