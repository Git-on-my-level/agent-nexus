package storage

import (
	"context"
	"database/sql"
)

// Receipt aggregates live beside the existing initiative projection. A view is
// the only serving relation: its synthetic ownership node inherits every receipt
// and the card, without making a private receipt restrict the canonical card.
func installBriefLoopFacts(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"events", "brief_initiatives", "brief_dirty", "resource_access_edges"} {
		has, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !has {
			return err
		}
	}
	for _, column := range []string{"loop_facts_json", "loop_valid_from", "loop_valid_until"} {
		has, err := sqliteTableHasColumn(ctx, tx, "brief_initiatives", column)
		if err != nil {
			return err
		}
		if !has {
			if _, err = tx.ExecContext(ctx, `ALTER TABLE brief_initiatives ADD COLUMN `+column+` TEXT`); err != nil {
				return err
			}
		}
	}
	subject := func(prefix string) string {
		return `substr(json_extract(` + prefix + `.payload_json,'$.subject_ref'),6)`
	}
	insertEdge := func(prefix string) string {
		return `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) SELECT 'brief_loop','loop-facts/'||` + subject(prefix) + `,'event:'||` + prefix + `.id WHERE ` + prefix + `.type='run_recorded' AND substr(json_extract(` + prefix + `.payload_json,'$.subject_ref'),1,5)='card:' ON CONFLICT DO NOTHING;`
	}
	dirty := func(prefix string) string {
		return `INSERT INTO brief_dirty(card_id) SELECT ` + subject(prefix) + ` WHERE ` + prefix + `.type='run_recorded' AND substr(json_extract(` + prefix + `.payload_json,'$.subject_ref'),1,5)='card:' ON CONFLICT DO NOTHING;`
	}
	clear := `DELETE FROM resource_access_edges WHERE source_kind='brief_loop' AND source_id='loop-facts/'||` + subject("OLD") + ` AND target_ref='event:'||OLD.id;`
	for _, query := range []string{
		`CREATE VIEW IF NOT EXISTS brief_loop_facts AS SELECT card_id,loop_facts_json facts_json,loop_valid_from valid_from,loop_valid_until valid_until FROM brief_initiatives WHERE loop_facts_json IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_brief_loop_expiry ON brief_initiatives(loop_valid_until,card_id) WHERE loop_valid_until IS NOT NULL`,
		`CREATE TRIGGER IF NOT EXISTS brief_loop_event_insert AFTER INSERT ON events BEGIN ` + insertEdge("NEW") + dirty("NEW") + ` END`,
		`CREATE TRIGGER IF NOT EXISTS brief_loop_event_update AFTER UPDATE OF payload_json,refs_json,type,ts,archived_at,trashed_at ON events BEGIN ` + clear + insertEdge("NEW") + dirty("OLD") + dirty("NEW") + ` END`,
		`CREATE TRIGGER IF NOT EXISTS brief_loop_event_delete AFTER DELETE ON events BEGIN ` + clear + dirty("OLD") + ` END`,
		`CREATE TRIGGER IF NOT EXISTS brief_loop_card_delete AFTER DELETE ON cards BEGIN DELETE FROM resource_access_edges WHERE source_kind='brief_loop' AND source_id='loop-facts/'||OLD.id; END`,
		`INSERT INTO resource_access_edges(source_kind,source_id,target_ref) SELECT 'brief_loop','loop-facts/'||substr(json_extract(payload_json,'$.subject_ref'),6),'event:'||id FROM events WHERE type='run_recorded' AND substr(json_extract(payload_json,'$.subject_ref'),1,5)='card:' ON CONFLICT DO NOTHING`,
		`INSERT INTO brief_dirty(card_id) SELECT id FROM cards WHERE true ON CONFLICT DO NOTHING`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

// Derived facts written before the incumbent rule must not serve candidate bests.
// Queue only existing caches; reads reject dirty facts until maintenance folds.
func requeueIncumbentLoopFacts(ctx context.Context, tx *sql.Tx) error {
	has, err := sqliteTableHasColumn(ctx, tx, "brief_initiatives", "loop_valid_until")
	if err != nil || !has {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO brief_dirty(card_id) SELECT card_id FROM brief_initiatives INDEXED BY idx_brief_loop_expiry WHERE loop_valid_until IS NOT NULL ON CONFLICT DO NOTHING`)
	return err
}
