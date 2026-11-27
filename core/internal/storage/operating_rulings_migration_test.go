package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestOperatingRulingMigrationFromPriorSchemas(t *testing.T) {
	for _, version := range []int{147, 150} {
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
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			ws, err := InitializeWorkspace(ctx, layout.RootDir)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if n := commentSearchCount(t, ws.DB(), `SELECT COUNT(*) FROM operating_ruling_positions`); n != 0 {
				t.Fatalf("legacy answers entered operating projection: %d", n)
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
			commentSearchExec(t, ws.DB(), `DELETE FROM schema_migrations WHERE version=151`)
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
