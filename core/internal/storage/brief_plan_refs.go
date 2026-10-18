package storage

import (
	"context"
	"database/sql"

	"agent-nexus-core/internal/resourceaccess"
)

// installBriefPlanRefs materializes the reverse card-step relation once, then
// maintains it atomically with plans. Ref spellings remain indexed so missing
// targets and handle changes also invalidate their dependents.
func installBriefPlanRefs(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{"progress_done", "progress_total"} {
		has, err := sqliteTableHasColumn(ctx, tx, "brief_initiatives", column)
		if err != nil {
			return err
		}
		if !has {
			if _, err = tx.ExecContext(ctx, `ALTER TABLE brief_initiatives ADD COLUMN `+column+` INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
	}
	has, err := sqliteTableHasColumn(ctx, tx, "brief_initiatives", "plan_provenance_json")
	if err != nil {
		return err
	}
	if !has {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE brief_initiatives ADD COLUMN plan_provenance_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return err
		}
	}
	var generated int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_xinfo('brief_initiatives') WHERE name='plan_provenance_id'`).Scan(&generated); err != nil {
		return err
	}
	if generated == 0 {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE brief_initiatives ADD COLUMN plan_provenance_id TEXT GENERATED ALWAYS AS ('plan-facts/' || card_id) VIRTUAL`); err != nil {
			return err
		}
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS brief_plan_refs(card_id TEXT NOT NULL, ref TEXT NOT NULL, PRIMARY KEY(card_id,ref))`,
		`CREATE INDEX IF NOT EXISTS idx_brief_plan_refs_target ON brief_plan_refs(ref,card_id)`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_refs_insert AFTER INSERT ON card_plans BEGIN
   INSERT INTO brief_plan_refs(card_id,ref) SELECT NEW.card_id,json_extract(value,'$.ref') FROM json_each(NEW.body_json,'$.steps') WHERE COALESCE(json_extract(value,'$.ref'),'')<>'' ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_refs_update AFTER UPDATE OF body_json ON card_plans BEGIN
   DELETE FROM brief_plan_refs WHERE card_id=OLD.card_id;
   INSERT INTO brief_plan_refs(card_id,ref) SELECT NEW.card_id,json_extract(value,'$.ref') FROM json_each(NEW.body_json,'$.steps') WHERE COALESCE(json_extract(value,'$.ref'),'')<>'' ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_refs_delete AFTER DELETE ON card_plans BEGIN
   DELETE FROM brief_plan_refs WHERE card_id=OLD.card_id;
   INSERT INTO brief_dirty(card_id) VALUES(OLD.card_id) ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_insert AFTER INSERT ON cards BEGIN
   INSERT INTO brief_dirty SELECT card_id FROM brief_plan_refs WHERE ref IN ('card:'||NEW.id,'card:'||NEW.handle) ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_update AFTER UPDATE OF column_key,handle,archived_at,trashed_at,board_id ON cards BEGIN
   INSERT INTO brief_dirty SELECT card_id FROM brief_plan_refs WHERE ref IN ('card:'||NEW.id,'card:'||NEW.handle,'card:'||OLD.handle,(SELECT json_extract(metadata_json,'$.source.url') FROM work_metadata WHERE card_id=NEW.id)) ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_delete AFTER DELETE ON cards BEGIN
   INSERT INTO brief_dirty SELECT card_id FROM brief_plan_refs WHERE ref IN ('card:'||OLD.id,'card:'||OLD.handle) ON CONFLICT DO NOTHING;
   DELETE FROM brief_plan_refs WHERE card_id=OLD.id;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_metadata_insert AFTER INSERT ON work_metadata BEGIN
   INSERT INTO brief_dirty SELECT r.card_id FROM brief_plan_refs r WHERE r.ref IN ('card:'||NEW.card_id,'card:'||(SELECT handle FROM cards WHERE id=NEW.card_id),json_extract(NEW.metadata_json,'$.source.url')) ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_metadata_update AFTER UPDATE OF metadata_json,latest_observation_id ON work_metadata BEGIN
   INSERT INTO brief_dirty SELECT r.card_id FROM brief_plan_refs r WHERE r.ref IN ('card:'||NEW.card_id,'card:'||(SELECT handle FROM cards WHERE id=NEW.card_id),json_extract(NEW.metadata_json,'$.source.url'),json_extract(OLD.metadata_json,'$.source.url')) ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_metadata_delete AFTER DELETE ON work_metadata BEGIN
   INSERT INTO brief_dirty SELECT r.card_id FROM brief_plan_refs r WHERE r.ref IN ('card:'||OLD.card_id,'card:'||(SELECT handle FROM cards WHERE id=OLD.card_id),json_extract(OLD.metadata_json,'$.source.url')) ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_board_lifecycle AFTER UPDATE OF trashed_at ON boards BEGIN
   INSERT INTO brief_dirty SELECT r.card_id FROM cards c CROSS JOIN brief_plan_refs r ON r.ref='card:'||c.id WHERE c.board_id=NEW.id ON CONFLICT DO NOTHING;
   INSERT INTO brief_dirty SELECT r.card_id FROM cards c CROSS JOIN brief_plan_refs r ON r.ref='card:'||c.handle WHERE c.board_id=NEW.id ON CONFLICT DO NOTHING;
   INSERT INTO brief_dirty SELECT r.card_id FROM cards c CROSS JOIN work_evidence_index e ON e.card_id=c.id CROSS JOIN brief_plan_refs r ON r.ref=e.lookup_key WHERE c.board_id=NEW.id ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_evidence_insert AFTER INSERT ON work_evidence_index BEGIN
   INSERT INTO brief_dirty SELECT card_id FROM brief_plan_refs WHERE ref=NEW.lookup_key ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_evidence_delete AFTER DELETE ON work_evidence_index BEGIN
   INSERT INTO brief_dirty SELECT card_id FROM brief_plan_refs WHERE ref=OLD.lookup_key ON CONFLICT DO NOTHING;
  END`,
		`INSERT INTO brief_plan_refs(card_id,ref) SELECT p.card_id,json_extract(step.value,'$.ref') FROM card_plans p,json_each(p.body_json,'$.steps') step WHERE COALESCE(json_extract(step.value,'$.ref'),'')<>'' ON CONFLICT DO NOTHING`,
		`UPDATE brief_initiatives SET plan_provenance_json=COALESCE((SELECT body_json FROM card_plans WHERE card_id=brief_initiatives.card_id),'{}')`,
		`INSERT INTO brief_dirty SELECT card_id FROM card_plans WHERE true ON CONFLICT DO NOTHING`,
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, source := range resourceaccess.OwnershipSources {
		if source.Kind == "brief_progress" {
			return installResourceAccessSourceEdges(ctx, tx, []resourceaccess.OwnershipSource{source})
		}
	}
	return nil
}
