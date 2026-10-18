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
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			revoked_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_grant_channel_declarations_channel ON grant_channel_declarations(channel, agent_id) WHERE revoked_at IS NULL`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("install channel declarations: %w", err)
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
