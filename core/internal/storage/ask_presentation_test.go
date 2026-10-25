package storage

import (
	"context"
	"fmt"
	"testing"
)

func TestUpgradeAskPresentationHeads(t *testing.T) {
	if testing.Short() {
		t.Skip("full storage migration and reopen")
	}
	for _, version := range []int{120, 122} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, version)
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
				if err = ws.DB().QueryRow(`SELECT count(*) FROM schema_migrations WHERE version IN (122,123)`).Scan(&count); err != nil || count != 2 {
					t.Fatalf("Watch and presentation migrations: count=%d err=%v", count, err)
				}

				if err = ws.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE (type='view' AND name='work_done_event_positions') OR (type='index' AND name='idx_events_card_done')`).Scan(&count); err != nil || count != 2 {
					t.Fatalf("Watch migration objects lost: count=%d err=%v", count, err)
				}
				ws.Close()
			}
		})
	}
}
