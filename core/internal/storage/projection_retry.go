package storage

import (
	"context"
	"database/sql"
)

func installProjectionRetryBackoff(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "topic_projection_refresh_status")
	if err != nil || !exists {
		return err
	} // Legacy partial-schema upgrades omit projections.
	for _, column := range []string{"retry_after", "failure_count"} {
		exists, err := sqliteTableHasColumn(ctx, tx, "topic_projection_refresh_status", column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE topic_projection_refresh_status ADD COLUMN "+column+" INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
		}
	}
	return nil
}
