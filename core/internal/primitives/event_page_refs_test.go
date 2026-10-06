package primitives

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestEventPageBatchesPublicRefsOnSingleConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO cards(id,handle,title,created_at,created_by,updated_at,updated_by) VALUES('target','public-target','Target','now','actor','now','actor')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 21; i++ {
		if _, err := ws.DB().ExecContext(ctx, `INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES(?,'card_updated','2026-01-01T00:00:00Z','actor','["card:target","card:missing"]','{"payload":{"target_ref":"card:target","text":"card:target","nested":{"refs":["card:target"]}}}')`, fmt.Sprintf("event-%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	counter.Reset()
	page, err := s.ListEventsPage(ctx, EventListFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 20 || page.NextCursor == "" {
		t.Fatalf("page: %+v", page)
	}
	for _, event := range page.Events {
		payload := event["payload"].(map[string]any)
		if !reflect.DeepEqual(event["refs"], []any{"card:missing", "card:public-target"}) || payload["target_ref"] != "card:public-target" || payload["text"] != "card:target" {
			t.Fatalf("presentation changed: %+v", event)
		}
	}
	if counter.Count() != 2 || counter.ReturnedRows() != 22 {
		t.Fatalf("unbounded hydration: queries=%d rows=%d", counter.Count(), counter.ReturnedRows())
	}
	// The presentation cache cannot survive a page, including a renamed handle.
	if _, err := db.ExecContext(ctx, `UPDATE cards SET handle='renamed' WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	page, err = s.ListEventsPage(ctx, EventListFilter{Limit: 20, Cursor: page.NextCursor})
	if err != nil || len(page.Events) != 1 || page.Events[0]["payload"].(map[string]any)["target_ref"] != "card:renamed" {
		t.Fatalf("stale page handles: %+v %v", page, err)
	}
}
