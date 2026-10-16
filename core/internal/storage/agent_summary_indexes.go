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
	_, err = tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_cards_agent_summary;
 CREATE INDEX IF NOT EXISTS idx_cards_agent_summary ON cards(assignee,updated_at DESC,id) WHERE archived_at IS NULL AND trashed_at IS NULL;
 DROP VIEW IF EXISTS agent_summary_card_positions;
 CREATE VIEW IF NOT EXISTS agent_summary_card_positions AS SELECT id,handle,assignee,updated_at FROM cards WHERE archived_at IS NULL AND trashed_at IS NULL`)
	return err
}
