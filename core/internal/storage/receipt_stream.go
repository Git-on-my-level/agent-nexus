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
		`CREATE VIEW IF NOT EXISTS agent_wakeup_snapshot_positions AS SELECT wakeup_id, thread_id, trigger_event_id, created_at FROM agent_wakeups`,
		`CREATE TABLE IF NOT EXISTS agent_wakeup_stream (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			thread_id TEXT NOT NULL,
			wakeup_id TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_wakeup_stream_thread_seq ON agent_wakeup_stream(thread_id, seq)`,
		`CREATE TABLE IF NOT EXISTS receipt_stream_state(singleton INTEGER PRIMARY KEY CHECK(singleton=1),log_start_seq INTEGER NOT NULL)`,
		`INSERT INTO receipt_stream_state SELECT 1,COALESCE(MAX(seq),0) FROM agent_wakeup_stream WHERE true ON CONFLICT DO NOTHING`,
	} {
		if _, err = tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	// Historical receipts stay in the canonical table. Fresh and accepted resume
	// cursors read bounded pages through its already-shipped ordering index; the
	// update log records only writes committed after this migration.
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
	return installReceiptStreamAccess(ctx, tx)
}
