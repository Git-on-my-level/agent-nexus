package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestOperatingRulingMigrationFromPriorSchemas(t *testing.T) {
	for _, version := range []int{147, 150, 151} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx := context.Background()
			layout := NewLayout(t.TempDir())
			if err := ensureLayout(layout); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", sqliteDSN(layout.DatabasePath))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			commentSearchExec(t, db, createMigrationsTableSQL)
			for _, m := range migrations {
				if m.Version > version {
					break
				}
				if err := applyMigration(ctx, db, m); err != nil {
					t.Fatal(err)
				}
			}
			if version >= 148 {
				commentSearchExec(t, db, `INSERT INTO standing_rulings(id,title_key,kind,question,answer,source_ask_id,created_by,created_at,uses,revoked_at,revoked_by)
				VALUES('answer-old','a','answer','A?','Yes','ask-a','human','2026-11-25',7,NULL,''),
				('revoked-old','b','answer','B?','No','ask-b','human','2026-11-25',3,'2026-11-26','human')`)
			}
			if version == 151 {
				commentSearchExec(t, db, `INSERT INTO standing_rulings(id,title_key,kind,question,created_by,created_at,ruling_id,status)
				VALUES('R-20261127-01','','operating','','human','2026-11-27T10:00:00Z','R-20261127-01','proposed'),
				('R-20261127-02','','operating','','human','2026-11-27T10:00:00.5Z','R-20261127-02','proposed')`)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			ws, err := InitializeWorkspace(ctx, layout.RootDir)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			wantOperating := 0
			if version == 151 {
				wantOperating = 2
			}
			if n := commentSearchCount(t, ws.DB(), `SELECT COUNT(*) FROM operating_ruling_positions`); n != wantOperating {
				t.Fatalf("operating projection has %d rows, want %d", n, wantOperating)
			}
			if version == 151 {
				const newest = `SELECT ruling_id FROM operating_ruling_positions WHERE status='proposed' ORDER BY rtrim(created_at,'Z') DESC,ruling_id LIMIT 1`
				var id, plan string
				var step, parent, unused int
				if err := ws.DB().QueryRowContext(ctx, newest).Scan(&id); err != nil || id != "R-20261127-02" {
					t.Fatalf("newest pre-upgrade ruling=%q err=%v", id, err)
				}
				if err := ws.DB().QueryRowContext(ctx, "EXPLAIN QUERY PLAN "+newest).Scan(&step, &parent, &unused, &plan); err != nil || !strings.Contains(plan, "idx_standing_rulings_proposed_utc") {
					t.Fatalf("pending order is not indexed: %q err=%v", plan, err)
				}
			}
			if version >= 148 {
				if n := commentSearchCount(t, ws.DB(), `SELECT COUNT(*) FROM standing_rulings WHERE ruling_id IS NULL AND scope='workspace' AND status='' AND kind='answer'
				 AND ((id='answer-old' AND uses=7 AND revoked_at IS NULL AND answer='Yes') OR
				      (id='revoked-old' AND uses=3 AND revoked_at='2026-11-26' AND revoked_by='human' AND answer='No'))`); n != 2 {
					t.Fatalf("migration changed legacy answers: %d matched", n)
				}
			}
			// Older upgrade fixtures and recovery replay the ledger against
			// already-present columns. The cursor key and revision must survive.
			var key string
			if err := ws.DB().QueryRowContext(ctx, `SELECT hex(cursor_key) FROM standing_ruling_revision`).Scan(&key); err != nil {
				t.Fatal(err)
			}
			commentSearchExec(t, ws.DB(), `UPDATE standing_ruling_revision SET version=17`)
			commentSearchExec(t, ws.DB(), `DELETE FROM schema_migrations WHERE version IN (151,152)`)
			if err := ws.Close(); err != nil {
				t.Fatal(err)
			}
			replayed, err := InitializeWorkspace(ctx, layout.RootDir)
			if err != nil {
				t.Fatal(err)
			}
			defer replayed.Close()
			var replayedKey string
			if err := replayed.DB().QueryRowContext(ctx, `SELECT hex(cursor_key) FROM standing_ruling_revision WHERE version=17`).Scan(&replayedKey); err != nil || key != replayedKey {
				t.Fatalf("replay rotated the projection authority: %v", err)
			}
		})
	}
}
