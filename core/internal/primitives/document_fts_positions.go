package primitives

import (
	"context"
	"database/sql"
)

// Both the FTS row and its lookup position change in the source transaction.
// Never inspect or mutate FTS5 shadow tables. An equality constraint on the
// virtual table's document_id UNINDEXED column would scan every document.
func clearDocumentFTSTx(ctx context.Context, tx documentFTSExecer, id string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_fts WHERE rowid IN (SELECT fts_rowid FROM document_search_fts_positions WHERE document_id=?)`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM document_search_fts_positions WHERE document_id=?`, id)
	return err
}

func recordDocumentFTSPosition(ctx context.Context, tx documentFTSExecer, id string, result sql.Result) error {
	rowid, err := result.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO document_search_fts_positions(document_id,fts_rowid) VALUES(?,?)`, id, rowid)
	return err
}
