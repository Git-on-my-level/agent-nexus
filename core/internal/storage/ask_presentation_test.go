package storage

import (
	"context"
	"testing"
)

func TestUpgrade120AskPresentationHeads(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 120)
	if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('retained-ask','human_attention_requested','2026-01-01T00:00:00Z','requester','[]','{"payload":{"title":"Original?"}}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		ws, err := InitializeWorkspace(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err = ws.DB().QueryRow(`SELECT count(*) FROM ask_presentation_heads`).Scan(&count); err != nil || count != 0 {
			t.Fatal(count, err)
		}
		var title string
		if err = ws.DB().QueryRow(`SELECT json_extract(payload_json,'$.payload.title') FROM events WHERE id='retained-ask'`).Scan(&title); err != nil || title != "Original?" {
			t.Fatal(title, err)
		}
		ws.Close()
	}
}
