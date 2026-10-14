package storage

import (
	"context"
	"database/sql"
)

func installTopicWorkspacePageIndexes(ctx context.Context, tx *sql.Tx) error {
	for _, index := range []struct {
		table, query string
		columns      []string
	}{
		{"ref_edges", `CREATE INDEX IF NOT EXISTS idx_ref_edges_source_page ON ref_edges(source_type,source_id,edge_type,id)`, []string{"source_type", "source_id", "edge_type", "id"}},
		{"ref_edges", `CREATE INDEX IF NOT EXISTS idx_ref_edges_target_page ON ref_edges(target_type,target_id,edge_type,id)`, []string{"target_type", "target_id", "edge_type", "id"}},
		{"documents", `CREATE INDEX IF NOT EXISTS idx_documents_thread_page ON documents(thread_id,id)`, []string{"thread_id", "id"}},
		{"cards", `CREATE INDEX IF NOT EXISTS idx_cards_parent_thread_page ON cards(parent_thread_id,id)`, []string{"parent_thread_id", "id"}},
	} {
		// Historical partial-schema imports may omit an entire feature table.
		// Normal workspaces have every table after the preceding migrations.
		exists, err := sqliteTableExists(ctx, tx, index.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		complete := true
		for _, column := range index.columns {
			hasColumn, err := sqliteTableHasColumn(ctx, tx, index.table, column)
			if err != nil {
				return err
			}
			complete = complete && hasColumn
		}
		if !complete {
			continue
		}
		if _, err := tx.ExecContext(ctx, index.query); err != nil {
			return err
		}
	}
	return nil
}
