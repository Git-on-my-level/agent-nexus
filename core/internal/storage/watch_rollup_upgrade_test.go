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
		`INSERT INTO events(id,type,ts,actor_id,payload_json) VALUES('older','card_moved','2026-10-24T12:00:00.1Z','writer','{"payload":{"card_id":"card-a","column_key":"done","from_column_key":"ready"}}')`,
		`INSERT INTO events(id,type,ts,actor_id,payload_json) VALUES('terminal','card_moved','2026-10-24T12:00:00.11Z','writer','{"payload":{"card_id":"card-a","column_key":"done","from_column_key":"ready"}}')`,
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
	if err = ws.DB().QueryRow(`SELECT id FROM work_done_event_positions WHERE card_id='card-a' ORDER BY anx_timestamp_key(ts) DESC,id DESC LIMIT 1`).Scan(&id); err != nil || id != "terminal" {
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
	if err = ws.DB().QueryRow(`SELECT count(*) FROM work_done_event_positions WHERE card_id='card-a'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("reopen count=%d err=%v", count, err)
	}
}

// A future release may introduce 121 after Watch's independent 122 has shipped.
// Exercise both real ledger runners, including the fenced ledger's digest.
func TestWatchRollupMigrationGap(t *testing.T) {
	original := migrations
	defer func() { migrations = original }()
	for _, fenced := range []bool{false, true} {
		name := "legacy"
		if fenced {
			name = "fenced"
		}
		t.Run(name, func(t *testing.T) {
			migrations = original
			ctx := context.Background()
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, 120)
			if fenced {
				fenceAdmissionHistory(t, root, db)
			} else {
				scopeMust(t, db.Close())
			}
			w, err := InitializeWorkspace(ctx, root)
			scopeMust(t, err)
			ledger := "schema_migrations"
			if fenced {
				ledger = "scope_schema_migrations"
			}
			var before string
			scopeMust(t, w.DB().QueryRow(`SELECT applied_at FROM `+ledger+` WHERE version=122`).Scan(&before))
			var count int
			scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM `+ledger+` WHERE version=121`).Scan(&count))
			if count != 0 {
				t.Fatal("121 unexpectedly applied")
			}
			scopeMust(t, w.Close())
			migrations = nil
			for _, m := range original {
				if m.Version == 122 {
					migrations = append(migrations, migration{Version: 121, Statements: []string{`CREATE TABLE late_migration_probe(id INTEGER PRIMARY KEY)`}})
				}
				migrations = append(migrations, m)
			}
			for reopen := 0; reopen < 2; reopen++ {
				w, err = InitializeWorkspace(ctx, root)
				scopeMust(t, err)
				scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM `+ledger+` WHERE version IN (121,122)`).Scan(&count))
				if count != 2 {
					t.Fatalf("ledger entries=%d", count)
				}
				var after string
				scopeMust(t, w.DB().QueryRow(`SELECT applied_at FROM `+ledger+` WHERE version=122`).Scan(&after))
				if before != after {
					t.Fatal("122 reapplied")
				}
				scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM late_migration_probe`).Scan(&count))
				scopeMust(t, w.Close())
			}
		})
	}
}
