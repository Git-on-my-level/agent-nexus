package storage

import (
	"context"
	"database/sql"
)

// FTS5 cannot index equality on an UNINDEXED column. Preserve every legacy
// row (including duplicates) in a compact, ordinary document-to-rowid index.
// This one-time projection scan reads only FTS IDs, never canonical histories.
func installDocumentFTSPositions(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS document_search_fts_positions(document_id NOT NULL,fts_rowid INTEGER NOT NULL UNIQUE,PRIMARY KEY(document_id,fts_rowid)) WITHOUT ROWID;
 DELETE FROM document_search_fts_positions;
 INSERT INTO document_search_fts_positions(document_id,fts_rowid) SELECT document_id,rowid FROM document_fts WHERE document_id IS NOT NULL ON CONFLICT DO NOTHING;`)
	return err
}
