package storage

import (
	"context"
	"database/sql"
)

func installTopicWorkspacePageIndexes(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS idx_ref_edges_source_page ON ref_edges(source_type,source_id,edge_type,id)`,
		`CREATE INDEX IF NOT EXISTS idx_ref_edges_target_page ON ref_edges(target_type,target_id,edge_type,id)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_thread_page ON documents(thread_id,id)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
