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

func upgradeAgentSummaryAndBriefAskIndexes(ctx context.Context, tx *sql.Tx) error {
	cardsExists, err := sqliteTableExists(ctx, tx, "cards")
	if err != nil {
		return err
	}
	if cardsExists {
		if _, err = tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_cards_agent_summary;
 CREATE INDEX IF NOT EXISTS idx_cards_agent_summary ON cards(assignee,updated_at DESC,id) WHERE archived_at IS NULL AND trashed_at IS NULL;
 DROP VIEW IF EXISTS agent_summary_card_positions;
 CREATE VIEW IF NOT EXISTS agent_summary_card_positions AS SELECT id,handle,assignee,updated_at FROM cards WHERE archived_at IS NULL AND trashed_at IS NULL`); err != nil {
			return err
		}
	}
	asksExists, err := sqliteTableExists(ctx, tx, "brief_asks")
	if err != nil {
		return err
	}
	blocksExists, err := sqliteTableExists(ctx, tx, "brief_ask_blocks")
	if err != nil {
		return err
	}
	if cardsExists && asksExists && blocksExists {
		if _, err = tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS brief_asks_open_blocks_insert;
 DROP TRIGGER IF EXISTS brief_asks_blocks_card_insert;
 DROP TRIGGER IF EXISTS brief_asks_blocks_card_update;
 DROP TRIGGER IF EXISTS brief_asks_blocks_card_delete; DROP INDEX IF EXISTS idx_brief_asks_open_page;
 CREATE INDEX IF NOT EXISTS idx_brief_asks_open_page ON brief_asks(state, decide_by, blocks_count DESC, CASE lower(trim(priority)) WHEN 'critical' THEN 0 WHEN 'p0' THEN 0 WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'p1' THEN 1 WHEN 'medium' THEN 2 WHEN 'p2' THEN 2 WHEN 'normal' THEN 2 WHEN 'low' THEN 3 WHEN 'p3' THEN 3 ELSE 4 END, created_at, ask_id)`); err != nil {
			return err
		}
	}
	adaptersExists, err := sqliteTableExists(ctx, tx, "series_adapters")
	if err != nil {
		return err
	}
	defsExists, err := sqliteTableExists(ctx, tx, "series_definitions")
	if err != nil {
		return err
	}
	if adaptersExists && defsExists {
		if _, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_series_adapters_active ON series_adapters(name) WHERE revoked_at IS NULL AND deleted_at IS NULL;
 CREATE INDEX IF NOT EXISTS idx_series_definitions_adapter ON series_definitions(adapter, name)`); err != nil {
			return err
		}
	}
	return upgradeSeriesCapacityIndexes(ctx, tx)
}
