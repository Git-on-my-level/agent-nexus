package storage

import (
	"context"
	"database/sql"
)

func applyMigration64BoardRole(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "boards")
	if err != nil || !exists {
		return err
	}
	role, err := sqliteTableHasColumn(ctx, tx, "boards", "role")
	if err != nil {
		return err
	}
	if !role {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE boards ADD COLUMN role TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_boards_role ON boards(role,id)`)
	return err
}
