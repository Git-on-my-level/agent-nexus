package storage

import (
	"context"
	"fmt"
	"testing"
)

func TestReceiptStreamUpgradeFromMainAndAppliedPreviews(t *testing.T) {
	for _, preview := range []string{"main", "ab382958", "eab89ee5"} {
		t.Run(preview, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, 66)
			for _, m := range migrations {
				if m.Version != 68 && !(preview != "main" && m.Version == 67) {
					continue
				}
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err = m.AfterApply(ctx, tx); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(`INSERT INTO schema_migrations VALUES(?,'preview-or-main')`, m.Version); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			if preview == "eab89ee5" {
				// The second preview changed an already-applied 67. Preserve its
				// actual footprint as well as the first preview's missing table.
				for _, q := range []string{
					`CREATE TABLE receipt_visibility_epoch(singleton INTEGER PRIMARY KEY CHECK(singleton=1),version INTEGER NOT NULL)`,
					`INSERT INTO receipt_visibility_epoch VALUES(1,7)`,
					`CREATE TRIGGER receipt_visibility_thread_pm AFTER UPDATE ON threads WHEN json_extract(OLD.body_json,'$.pm_actor_id') IS NOT json_extract(NEW.body_json,'$.pm_actor_id') BEGIN UPDATE receipt_visibility_epoch SET version=version+1 WHERE singleton=1; END`,
					`CREATE TRIGGER receipt_visibility_event_trash AFTER UPDATE ON events WHEN OLD.trashed_at IS NOT NEW.trashed_at BEGIN UPDATE receipt_visibility_epoch SET version=version+1 WHERE singleton=1; END`,
				} {
					if _, err := db.Exec(q); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, q := range []string{
				`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('thread','now','owner','{}')`,
				`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at) VALUES('historical','requested','unread','agent','target','thread','[]','now','now')`,
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			for reopen := 0; reopen < 2; reopen++ {
				ws, err := InitializeWorkspace(ctx, root)
				if err != nil {
					t.Fatal(err)
				}
				var count int
				for _, check := range []struct {
					sql  string
					want int
				}{
					{`SELECT count(*) FROM schema_migrations WHERE version=69`, 1},
					{`SELECT count(*) FROM schema_migrations WHERE version<=66 AND applied_at<>'historical-main'`, 0},
					{`SELECT count(*) FROM agent_wakeup_stream WHERE wakeup_id='historical'`, 1},
					{`SELECT count(*) FROM receipt_access_epoch`, 1},
					{`SELECT count(*) FROM receipt_visibility_epoch`, 1},
					{`SELECT count(*) FROM agent_wakeup_snapshot_positions WHERE trigger_event_id='' AND wakeup_id='historical'`, 1},
					{`SELECT count(*) FROM receipt_trigger_positions`, 0},
				} {
					if err = ws.DB().QueryRow(check.sql).Scan(&count); err != nil || count != check.want {
						t.Fatalf("reopen %d: %s = %d, want %d: %v", reopen, check.sql, count, check.want, err)
					}
				}
				id := fmt.Sprintf("new-%d", reopen)
				if _, err = ws.DB().Exec(`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at) VALUES(?,'requested','unread','agent','target','thread','[]','now','now')`, id); err != nil {
					t.Fatal(err)
				}
				if err = ws.DB().QueryRow(`SELECT count(*) FROM agent_wakeup_stream WHERE wakeup_id=?`, id).Scan(&count); err != nil || count != 1 {
					t.Fatalf("new write not logged once: %d %v", count, err)
				}
				if err = ws.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
