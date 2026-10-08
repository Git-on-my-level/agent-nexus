package storage

import (
	"context"
	"database/sql"
)

func indexAgentSummaryCards(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "cards")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_cards_agent_summary ON cards(assignee,id) WHERE archived_at IS NULL AND trashed_at IS NULL;
 CREATE VIEW IF NOT EXISTS agent_summary_card_positions AS SELECT id,handle,assignee FROM cards WHERE archived_at IS NULL AND trashed_at IS NULL`)
	return err
}
