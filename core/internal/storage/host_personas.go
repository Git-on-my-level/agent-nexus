package storage

import (
	"context"
	"database/sql"
)

// installHostPersonaArchive adds the archive tombstone to host personas.
//
// archived_at marks a persona whose name stays reserved on its host while it
// cannot authenticate; archived_reason says who archived it (manual: a human,
// inactive: the maintenance sweep). Both are identity metadata about the
// principal, never content. DDL only; no rows are scanned.
func installHostPersonaArchive(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "host_agents")
	if err != nil || !exists {
		return err
	}
	for _, column := range []string{"archived_at", "archived_reason"} {
		has, err := sqliteTableHasColumn(ctx, tx, "host_agents", column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE host_agents ADD COLUMN `+column+` TEXT`); err != nil {
			return err
		}
	}
	return nil
}
