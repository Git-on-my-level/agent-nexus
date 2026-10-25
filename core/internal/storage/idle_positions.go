package storage

import (
	"agent-nexus-core/internal/contentusage"
	"context"
	"database/sql"
)

func installIdleMaintenancePositions(ctx context.Context, tx *sql.Tx) error {
	// Materialize SQLite's historical chronological key. Unlike an expression
	// index on julianday(ts), this also admits legacy non-RFC timestamp rows
	// without SQLite's non-deterministic-index error. Canonical events are intact.
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS router_message_positions(event_id TEXT PRIMARY KEY,sort_time REAL);
 CREATE INDEX IF NOT EXISTS idx_router_message_positions_order ON router_message_positions(sort_time,event_id);
 DELETE FROM router_message_positions;
 INSERT INTO router_message_positions SELECT id,julianday(ts) FROM events WHERE type='message_posted' ON CONFLICT(event_id) DO UPDATE SET sort_time=excluded.sort_time;
 CREATE TRIGGER IF NOT EXISTS router_message_insert AFTER INSERT ON events WHEN NEW.type='message_posted' BEGIN
 INSERT INTO router_message_positions VALUES(NEW.id,julianday(NEW.ts));END;
 CREATE TRIGGER IF NOT EXISTS router_message_update AFTER UPDATE OF id,type,ts ON events WHEN OLD.id IS NOT NEW.id OR OLD.type IS NOT NEW.type OR OLD.ts IS NOT NEW.ts BEGIN
 DELETE FROM router_message_positions WHERE event_id=OLD.id;
 INSERT INTO router_message_positions SELECT NEW.id,julianday(NEW.ts) WHERE NEW.type='message_posted';END;
 CREATE TRIGGER IF NOT EXISTS router_message_delete AFTER DELETE ON events WHEN OLD.type='message_posted' BEGIN DELETE FROM router_message_positions WHERE event_id=OLD.id;END;`)
	if err != nil {
		return err
	}
	if err = installDocumentFTSPositions(ctx, tx); err != nil {
		return err
	}
	return contentusage.Install(ctx, tx)
}
