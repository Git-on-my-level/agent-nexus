package storage

import (
	"context"
	"testing"
)

func TestWatchRollupUpgradeIndexesExistingLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 120)
	for _, sql := range []string{
		`INSERT INTO events(id,type,ts,actor_id,payload_json) VALUES('terminal','card_moved','2026-10-24T12:00:00Z','writer','{"payload":{"card_id":"card-a","column_key":"done","from_column_key":"ready"}}')`,
		`INSERT INTO events(id,type,ts,actor_id,payload_json) VALUES('reorder','card_moved','2026-10-25T12:00:00Z','writer','{"payload":{"card_id":"card-a","column_key":"done","from_column_key":"done"}}')`,
		`INSERT INTO events(id,type,ts,actor_id,payload_json,trashed_at) VALUES('trashed','card_moved','2026-10-26T12:00:00Z','writer','{"payload":{"card_id":"card-a","column_key":"done","from_column_key":"ready"}}','2026-10-26T12:01:00Z')`,
	} {
		if _, err := db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err = ws.DB().QueryRow(`SELECT id FROM work_done_event_positions WHERE card_id='card-a' ORDER BY ts DESC,id DESC LIMIT 1`).Scan(&id); err != nil || id != "terminal" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var count int
	if err = ws.DB().QueryRow(`SELECT count(*) FROM work_done_event_positions WHERE card_id='card-a'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("reopen count=%d err=%v", count, err)
	}
}
