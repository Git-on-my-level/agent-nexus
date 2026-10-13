package storage

import (
	"context"
	"database/sql"
)

// Preview schemas used a connection-local function in source triggers. Replace
// those triggers and add bounded raw capture without scanning historical rows.
func upgradeExternalCommentSearch(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "comment_search_heads")
	if err != nil || !exists {
		return err
	}
	for _, field := range []struct{ name, definition string }{
		{"raw_text", "BLOB NOT NULL DEFAULT X''"},
		{"raw_truncated", "INTEGER NOT NULL DEFAULT 0"},
		// Existing rows are already normalized under their version-2 certification.
		{"ready", "INTEGER NOT NULL DEFAULT 1"},
	} {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('comment_search_heads') WHERE name=?`, field.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err = tx.ExecContext(ctx, "ALTER TABLE comment_search_heads ADD COLUMN "+field.name+" "+field.definition); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS comment_search_event_insert;DROP TRIGGER IF EXISTS comment_search_event_update;DROP TRIGGER IF EXISTS comment_search_event_delete;DROP TRIGGER IF EXISTS document_search_document_update`); err != nil {
		return err
	}
	return installCommentSearch(ctx, tx)
}
