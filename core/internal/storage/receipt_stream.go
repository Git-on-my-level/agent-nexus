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
		// Internal position metadata, never an API resource. Payloads load
		// separately through the scoped agent_wakeups relation.
		`CREATE VIEW IF NOT EXISTS agent_wakeup_snapshot_positions AS SELECT wakeup_id, thread_id, created_at FROM agent_wakeups`,
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
	return nil
}
