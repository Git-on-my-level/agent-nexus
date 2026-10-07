package storage

import (
	"context"
	"database/sql"
)

func indexBoundedEventStreamReads(ctx context.Context, tx *sql.Tx) error {
	// Legacy migration fixtures/imports may contain only a subset of tables.
	exists, err := sqliteTableExists(ctx, tx, "events")
	if err != nil || !exists {
		return err
	}
	for _, query := range []string{
		`CREATE INDEX IF NOT EXISTS idx_events_stream_order ON events(anx_timestamp_key(ts),id)`,
		// Internal position metadata, never an API resource. Payloads must
		// be loaded separately through the scoped events relation.
		`CREATE VIEW IF NOT EXISTS event_stream_positions AS SELECT id,ts FROM events`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
