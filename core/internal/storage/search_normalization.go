package storage

import (
	"agent-nexus-core/internal/scopesearch"
	"context"
	"database/sql"
	"fmt"
)

// Preview-82 databases contain case-only projections. Invalidate certification
// using singleton metadata only; online workers re-normalize canonical sources
// in bounded, resumable chunks. No history is read on the startup path.
func upgradeSearchNormalization(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"comment_search_job", "document_search_state"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name='normalization_version'`, table).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err = tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN normalization_version INTEGER NOT NULL DEFAULT 1`); err != nil {
				return err
			}
		}
		extra := ""
		if table == "document_search_state" {
			extra = ",generation=generation+1"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE `+table+` SET normalization_version=?,cursor=NULL,done=0`+extra+` WHERE normalization_version<>?`, scopesearch.NormalizationVersion, scopesearch.NormalizationVersion); err != nil {
			return fmt.Errorf("reset search normalization: %w", err)
		}
	}
	return nil
}
