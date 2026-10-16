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

const briefOpenBlocksCountSQL = `(SELECT COUNT(*) FROM (
 SELECT trim(j.value) AS ref
 FROM json_each(CASE WHEN json_type(brief_asks.blocks_json)='array' THEN brief_asks.blocks_json ELSE '[]' END) j
 WHERE typeof(j.value)='text' AND trim(j.value)<>''
 LIMIT 20
) b WHERE substr(b.ref,1,5)<>'card:'
 OR EXISTS(SELECT 1 FROM cards c WHERE c.id=substr(b.ref,6) AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')='' AND c.column_key NOT IN ('done','published','closed','resolved','merged','cancelled'))
 OR EXISTS(SELECT 1 FROM cards c WHERE c.handle IS NOT NULL AND trim(c.handle)<>'' AND c.handle=substr(b.ref,6) AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')='' AND c.column_key NOT IN ('done','published','closed','resolved','merged','cancelled')))`

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
 CREATE TRIGGER brief_asks_open_blocks_insert AFTER INSERT ON brief_asks WHEN NEW.blocks_json<>'[]' BEGIN UPDATE brief_asks SET blocks_count=`+briefOpenBlocksCountSQL+` WHERE ask_id=NEW.ask_id; END;
 DROP TRIGGER IF EXISTS brief_asks_blocks_card_insert;
 CREATE TRIGGER brief_asks_blocks_card_insert AFTER INSERT ON cards BEGIN UPDATE brief_asks SET blocks_count=`+briefOpenBlocksCountSQL+` WHERE state='open' AND ask_id IN (SELECT ask_id FROM brief_ask_blocks WHERE ref IN ('card:'||NEW.id,'card:'||COALESCE(NEW.handle,''))); END;
 DROP TRIGGER IF EXISTS brief_asks_blocks_card_update;
 CREATE TRIGGER brief_asks_blocks_card_update AFTER UPDATE OF column_key,archived_at,trashed_at,handle ON cards BEGIN UPDATE brief_asks SET blocks_count=`+briefOpenBlocksCountSQL+` WHERE state='open' AND ask_id IN (SELECT ask_id FROM brief_ask_blocks WHERE ref IN ('card:'||NEW.id,'card:'||OLD.id,'card:'||COALESCE(NEW.handle,''),'card:'||COALESCE(OLD.handle,''))); END;
 DROP TRIGGER IF EXISTS brief_asks_blocks_card_delete;
 CREATE TRIGGER brief_asks_blocks_card_delete AFTER DELETE ON cards BEGIN UPDATE brief_asks SET blocks_count=`+briefOpenBlocksCountSQL+` WHERE state='open' AND ask_id IN (SELECT ask_id FROM brief_ask_blocks WHERE ref IN ('card:'||OLD.id,'card:'||COALESCE(OLD.handle,''))); END;
 UPDATE brief_asks SET blocks_count=`+briefOpenBlocksCountSQL+` WHERE blocks_json<>'[]';
 DROP INDEX IF EXISTS idx_brief_asks_open_page;
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
	return nil
}
