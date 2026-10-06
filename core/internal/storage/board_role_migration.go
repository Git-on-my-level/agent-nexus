package storage

import (
	"context"
	"database/sql"
)

func applyMigration61BoardRole(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "boards")
	if err != nil || !exists {
		return err
	}
	role, err := sqliteTableHasColumn(ctx, tx, "boards", "role")
	if err != nil || role {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE boards ADD COLUMN role TEXT NOT NULL DEFAULT ''`)
	return err
}
