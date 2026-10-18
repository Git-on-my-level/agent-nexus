package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Channel declarations (migration 109).
//
// adapter_channels gains how a channel accepts deliveries: addressing
// ("settings" uses the address a person saves; "principal" needs none and is
// on by default for every human), the kinds it accepts and a short label.
// grant_channel_declarations holds the one channel an agent created by an
// external identity grant may declare; it is not a series adapter because
// it has no enrolled host. idx_agents_active_humans is the bounded walk of
// active human principals a principal-addressed channel reaches.
func installChannelDeclarations(ctx context.Context, tx *sql.Tx) error {
	if ok, err := sqliteTableExists(ctx, tx, "adapter_channels"); err != nil {
		return err
	} else if ok {
		for _, field := range []struct{ column, definition string }{
			{"addressing", "TEXT NOT NULL DEFAULT 'settings'"},
			{"kinds", "TEXT NOT NULL DEFAULT 'push,digest'"},
			{"label", "TEXT NOT NULL DEFAULT ''"},
		} {
			has, err := sqliteTableHasColumn(ctx, tx, "adapter_channels", field.column)
			if err != nil {
				return err
			}
			if has {
				continue
			}
			if _, err := tx.ExecContext(ctx, `ALTER TABLE adapter_channels ADD COLUMN `+field.column+` `+field.definition); err != nil {
				return fmt.Errorf("add adapter_channels.%s: %w", field.column, err)
			}
		}
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS grant_channel_declarations (
			agent_id TEXT PRIMARY KEY,
			issuer TEXT NOT NULL DEFAULT '',
			channel TEXT NOT NULL,
			addressing TEXT NOT NULL DEFAULT 'settings',
			kinds TEXT NOT NULL DEFAULT 'push,digest',
			label TEXT NOT NULL DEFAULT '',
			manage_url TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			revoked_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_grant_channel_declarations_channel ON grant_channel_declarations(channel, agent_id) WHERE revoked_at IS NULL`,
		// Materialized readability for per-principal attention counts (see
		// internal/delivery/readability.go): one row per notified principal and
		// open ask. The heartbeat reads it by primary-key prefix; the refresh
		// step walks idx_principal_ask_readability_refresh oldest epoch first.
		`CREATE TABLE IF NOT EXISTS principal_ask_readability (
			principal_id TEXT NOT NULL,
			request_event_id TEXT NOT NULL,
			readable INTEGER NOT NULL DEFAULT 0,
			checked_epoch INTEGER NOT NULL DEFAULT -1,
			checked_at TEXT NOT NULL DEFAULT '',
			-- Copied from ask_deadlines (kept in sync by triggers) so per-
			-- principal counts are index-only aggregates.
			decide_by TEXT,
			escalated INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(principal_id, request_event_id)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_principal_ask_readability_refresh ON principal_ask_readability(checked_epoch, checked_at)`,
		// due and overdue: one covering range per principal.
		`CREATE INDEX IF NOT EXISTS idx_principal_ask_readability_due ON principal_ask_readability(principal_id, readable, decide_by)`,
		// earliest unescalated deadline: one seek per principal.
		`CREATE INDEX IF NOT EXISTS idx_principal_ask_readability_earliest ON principal_ask_readability(principal_id, readable, escalated, decide_by)`,
		`CREATE INDEX IF NOT EXISTS idx_principal_ask_readability_ask ON principal_ask_readability(request_event_id)`,
		`CREATE TABLE IF NOT EXISTS principal_readability_coverage (
			principal_id TEXT PRIMARY KEY,
			cursor TEXT NOT NULL DEFAULT '',
			complete INTEGER NOT NULL DEFAULT 0
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS ask_readability_queue (
			request_event_id TEXT PRIMARY KEY
		) WITHOUT ROWID`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("install channel declarations: %w", err)
		}
	}
	if ok, err := sqliteTableExists(ctx, tx, "ask_deadlines"); err != nil {
		return err
	} else if ok {
		for _, statement := range []string{
			`CREATE TRIGGER IF NOT EXISTS ask_readability_on_open AFTER INSERT ON ask_deadlines BEGIN
				INSERT INTO ask_readability_queue(request_event_id) VALUES(NEW.request_event_id) ON CONFLICT DO NOTHING;
			END`,
			`CREATE TRIGGER IF NOT EXISTS ask_readability_on_deadline AFTER UPDATE OF decide_by, escalated_at ON ask_deadlines BEGIN
				UPDATE principal_ask_readability SET decide_by=NEW.decide_by, escalated=(NEW.escalated_at IS NOT NULL)
					WHERE request_event_id=NEW.request_event_id;
			END`,
			`CREATE TRIGGER IF NOT EXISTS ask_readability_on_close AFTER DELETE ON ask_deadlines BEGIN
				DELETE FROM principal_ask_readability WHERE request_event_id=OLD.request_event_id;
				DELETE FROM ask_readability_queue WHERE request_event_id=OLD.request_event_id;
			END`,
			// Asks already open when this migration runs are covered by each
			// principal's ledger walk.
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("install ask readability triggers: %w", err)
			}
		}
	}
	if ok, err := sqliteTableExists(ctx, tx, "agents"); err != nil {
		return err
	} else if ok {
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_agents_active_humans ON agents(actor_id) WHERE revoked_at IS NULL AND json_extract(metadata_json,'$.principal_kind')='human'`); err != nil {
			return fmt.Errorf("install active human index: %w", err)
		}
	}
	return nil
}
