package storage

import (
	"context"
	"database/sql"
)

func indexBoundedReceiptStreamReads(ctx context.Context, tx *sql.Tx) error {
	// Legacy fixtures can apply this migration against a partial schema.
	exists, err := sqliteTableExists(ctx, tx, "agent_wakeups")
	if err != nil || !exists {
		return err
	}
	for _, query := range []string{
		`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_thread_trigger_created ON agent_wakeups (thread_id, created_at ASC, wakeup_id ASC)`,
		// Payload pages seek wakeup_id directly. The created-at index is only
		// the snapshot position order.
		`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_thread_wakeup ON agent_wakeups (thread_id, wakeup_id)`,
		// Internal position metadata, never an API resource. Payloads load
		// separately through the scoped agent_wakeups relation.
		`CREATE VIEW IF NOT EXISTS agent_wakeup_snapshot_positions AS SELECT wakeup_id, thread_id, trigger_event_id, created_at FROM agent_wakeups`,
		`CREATE TABLE IF NOT EXISTS receipt_visibility_epoch(singleton INTEGER PRIMARY KEY CHECK(singleton=1), version INTEGER NOT NULL)`,
		`INSERT INTO receipt_visibility_epoch(singleton, version) VALUES(1, 0) ON CONFLICT DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS agent_wakeup_stream (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			thread_id TEXT NOT NULL,
			wakeup_id TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_wakeup_stream_thread_seq ON agent_wakeup_stream(thread_id, seq)`,
	} {
		if _, err = tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	// Backfill before the triggers so existing rows are recorded once.
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_wakeup_stream(thread_id, wakeup_id)
		SELECT thread_id, wakeup_id FROM agent_wakeups ORDER BY created_at, wakeup_id`); err != nil {
		return err
	}
	for _, query := range []string{
		`CREATE TRIGGER IF NOT EXISTS agent_wakeups_stream_insert AFTER INSERT ON agent_wakeups
			BEGIN
				INSERT INTO agent_wakeup_stream(thread_id, wakeup_id) VALUES (NEW.thread_id, NEW.wakeup_id);
			END`,
		`CREATE TRIGGER IF NOT EXISTS agent_wakeups_stream_update AFTER UPDATE ON agent_wakeups
			BEGIN
				INSERT INTO agent_wakeup_stream(thread_id, wakeup_id) VALUES (NEW.thread_id, NEW.wakeup_id);
			END`,
	} {
		if _, err = tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	// Wakeup writes already append to the update log. This epoch moves only
	// when inherited visibility can change an existing receipt. Legacy fixtures
	// can reach this migration without the early threads and events tables.
	visibility := []struct{ table, query string }{
		{"threads", `CREATE TRIGGER IF NOT EXISTS receipt_visibility_thread_pm AFTER UPDATE ON threads
			WHEN COALESCE(json_extract(OLD.body_json,'$.pm_actor_id'),'') <> COALESCE(json_extract(NEW.body_json,'$.pm_actor_id'),'')
			BEGIN
				UPDATE receipt_visibility_epoch SET version=version+1 WHERE singleton=1;
			END`},
		{"events", `CREATE TRIGGER IF NOT EXISTS receipt_visibility_event_trash AFTER UPDATE ON events
			WHEN COALESCE(OLD.trashed_at,'') <> COALESCE(NEW.trashed_at,'')
			BEGIN
				UPDATE receipt_visibility_epoch SET version=version+1 WHERE singleton=1;
			END`},
	}
	for _, trigger := range visibility {
		exists, err = sqliteTableExists(ctx, tx, trigger.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err = tx.ExecContext(ctx, trigger.query); err != nil {
			return err
		}
	}
	return nil
}
