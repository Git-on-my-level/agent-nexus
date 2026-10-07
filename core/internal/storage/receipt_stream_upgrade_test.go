package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestReceiptStreamUpgradeFromReleasedAndMain(t *testing.T) {
	type work struct {
		changes, pages int
		elapsed        time.Duration
	}
	var small work
	for _, fixture := range []struct {
		name      string
		count     int
		foreign67 bool
	}{
		{"v0.12.19-1k", 1000, false},
		{"main-with-inbox-67", 1000, true},
		{"v0.12.19-100k", 100000, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if fixture.count == 100000 && testing.Short() {
				t.Skip("real 100k released-schema startup fixture")
			}
			ctx := context.Background()
			root := t.TempDir()
			// v0.12.19 and main both ship through 68, without receipt 67.
			// Build unchanged history, rather than downgrading a preview.
			db := mainHistoryDatabase(t, root, 68)
			if fixture.foreign67 {
				// #308's ledger entry must not suppress the receipt schema.
				if _, err := db.Exec(`CREATE TABLE inbox_migration_marker(value TEXT); INSERT INTO inbox_migration_marker VALUES('preserve'); INSERT INTO schema_migrations VALUES(67,'inbox')`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('thread','now','owner','{}')`); err != nil {
				t.Fatal(err)
			}
			// Retain released ownership/identity triggers during fixture writes.
			// Migration 69 must not rebuild their populated indexes either.
			if _, err := db.Exec(`WITH RECURSIVE history(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM history WHERE n<?)
			 INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at)
			 SELECT printf('historical-%06d',n),'requested','unread','agent','target','thread','[]','now','now' FROM history`, fixture.count); err != nil {
				t.Fatal(err)
			}
			var beforePages int
			if err := db.QueryRow(`PRAGMA page_count`).Scan(&beforePages); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			ws, err := InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			got := work{elapsed: time.Since(started)}
			var pages int
			if err = ws.DB().QueryRow(`SELECT total_changes()`).Scan(&got.changes); err != nil {
				t.Fatal(err)
			}
			if err = ws.DB().QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
				t.Fatal(err)
			}
			got.pages = pages - beforePages
			t.Logf("receipts=%d startup=%s writes=%d allocated-pages=%d", fixture.count, got.elapsed, got.changes, got.pages)
			if fixture.name == "v0.12.19-1k" {
				small = got
			} else if fixture.count == 100000 {
				// Writes detect backfill; pages also detect CREATE INDEX over
				// populated history. Timing is a secondary, generous bound.
				if got.changes != small.changes || got.pages > small.pages+8 {
					t.Fatalf("startup grew with history: 1k=%+v 100k=%+v", small, got)
				}
				if got.elapsed > time.Second && got.elapsed > small.elapsed*20 {
					t.Fatalf("startup scanned history: 1k=%s 100k=%s", small.elapsed, got.elapsed)
				}
			}
			for reopen := 0; reopen < 2; reopen++ {
				for _, check := range []struct {
					sql  string
					want int
				}{
					{`SELECT count(*) FROM schema_migrations WHERE version=69`, 1},
					{`SELECT count(*) FROM schema_migrations WHERE version<=68 AND applied_at NOT IN ('historical-main','inbox')`, 0},
					{`SELECT count(*) FROM agent_wakeup_stream WHERE wakeup_id LIKE 'historical-%'`, 0},
					{`SELECT log_start_seq FROM receipt_stream_state`, 0},
					{`SELECT count(*) FROM agent_wakeup_snapshot_positions`, fixture.count + reopen},
					{`SELECT count(*) FROM receipt_access_epoch`, 1},
					{`SELECT count(*) FROM receipt_visibility_epoch`, 1},
				} {
					var n int
					if err = ws.DB().QueryRow(check.sql).Scan(&n); err != nil || n != check.want {
						t.Fatalf("reopen %d: %s=%d want %d: %v", reopen, check.sql, n, check.want, err)
					}
				}
				if fixture.foreign67 {
					var marker string
					if err = ws.DB().QueryRow(`SELECT value FROM inbox_migration_marker`).Scan(&marker); err != nil || marker != "preserve" {
						t.Fatalf("inbox migration lost: %q %v", marker, err)
					}
				}
				id := fmt.Sprintf("new-%d", reopen)
				if _, err = ws.DB().Exec(`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at) VALUES(?,'requested','unread','agent','target','thread','[]','now','now')`, id); err != nil {
					t.Fatal(err)
				}
				var n int
				if err = ws.DB().QueryRow(`SELECT count(*) FROM agent_wakeup_stream WHERE wakeup_id=?`, id).Scan(&n); err != nil || n != 1 {
					t.Fatalf("new write logged %d times: %v", n, err)
				}
				if err = ws.Close(); err != nil {
					t.Fatal(err)
				}
				if reopen == 0 {
					ws, err = InitializeWorkspace(ctx, root)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
