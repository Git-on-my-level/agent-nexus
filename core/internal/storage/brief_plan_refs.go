package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_update AFTER UPDATE OF column_key,handle,archived_at,trashed_at,board_id ON cards
   WHEN NEW.column_key IS NOT OLD.column_key OR NEW.handle IS NOT OLD.handle OR NEW.archived_at IS NOT OLD.archived_at OR NEW.trashed_at IS NOT OLD.trashed_at OR NEW.board_id IS NOT OLD.board_id BEGIN
   INSERT INTO brief_dirty SELECT card_id FROM brief_plan_refs WHERE ref IN ('card:'||NEW.id,'card:'||NEW.handle,'card:'||OLD.handle,(SELECT json_extract(metadata_json,'$.source.url') FROM work_metadata WHERE card_id=NEW.id)) ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_before_delete BEFORE DELETE ON cards BEGIN
   INSERT INTO brief_dirty SELECT r.card_id FROM work_evidence_index e CROSS JOIN brief_plan_refs r ON r.ref=e.lookup_key WHERE e.card_id=OLD.id ON CONFLICT DO NOTHING;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_delete AFTER DELETE ON cards BEGIN
   INSERT INTO brief_dirty SELECT card_id FROM brief_plan_refs WHERE ref IN ('card:'||OLD.id,'card:'||OLD.handle) ON CONFLICT DO NOTHING;
   DELETE FROM brief_plan_refs WHERE card_id=OLD.id;
  END`,
		`CREATE TRIGGER IF NOT EXISTS brief_plan_target_board_lifecycle AFTER UPDATE OF trashed_at ON boards WHEN NEW.trashed_at IS NOT OLD.trashed_at BEGIN
   INSERT INTO brief_dirty SELECT r.card_id FROM cards c CROSS JOIN brief_plan_refs r ON r.ref='card:'||c.id WHERE c.board_id=NEW.id ON CONFLICT DO NOTHING;
   INSERT INTO brief_dirty SELECT r.card_id FROM cards c CROSS JOIN brief_plan_refs r ON r.ref='card:'||c.handle WHERE c.board_id=NEW.id ON CONFLICT DO NOTHING;
   INSERT INTO brief_dirty SELECT r.card_id FROM cards c CROSS JOIN work_evidence_index e ON e.card_id=c.id CROSS JOIN brief_plan_refs r ON r.ref=e.lookup_key WHERE c.board_id=NEW.id ON CONFLICT DO NOTHING;
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
	if err := installBriefPlanTargetTriggers(ctx, tx); err != nil {
		return err
	}
	for _, source := range resourceaccess.OwnershipSources {
		if source.Kind == "brief_progress" {
			return installResourceAccessSourceEdges(ctx, tx, []resourceaccess.OwnershipSource{source})
		}
	}
	return nil
}

// Compare only inputs that determine referenced step state and evidence identity.
// Poll timestamps and refresh bookkeeping do not change these inputs. Read the
// old/new observation by primary key, never their history or workspace contents.
func briefPlanTargetEntries(row string) string {
	obs := `(SELECT body_json FROM work_observations WHERE id=` + row + `.latest_observation_id)`
	return `SELECT value AS evidence FROM json_each(` + row + `.metadata_json,'$.source_refs') WHERE type='object' AND CAST(key AS INTEGER)<2000
 UNION ALL SELECT value FROM json_each(` + obs + `,'$.evidence') WHERE type='object' AND CAST(key AS INTEGER)<2000
 UNION ALL SELECT json_patch(COALESCE(json_extract(` + row + `.metadata_json,'$.source'),'{}'),json_object(
 'authority',` + row + `.authority,'connection_id',COALESCE(` + row + `.connection_id,''),'native_id',COALESCE(` + row + `.native_id,''),
 'status',COALESCE(NULLIF(json_extract(` + obs + `,'$.facts.native_status'),''),json_extract(` + obs + `,'$.facts.phase'),''),
 'phase',COALESCE(json_extract(` + obs + `,'$.facts.phase'),''),
 'identifier',COALESCE(json_extract(` + obs + `,'$.facts.identifier'),json_extract(` + row + `.metadata_json,'$.source.identifier'),''),
 'identifier_aliases',json(CASE WHEN json_type(` + obs + `,'$.facts.identifier_aliases')='array' THEN json_extract(` + obs + `,'$.facts.identifier_aliases') WHEN json_type(` + row + `.metadata_json,'$.source.identifier_aliases')='array' THEN json_extract(` + row + `.metadata_json,'$.source.identifier_aliases') ELSE '[]' END),
 'aliases',json(CASE WHEN json_type(` + obs + `,'$.facts.aliases')='array' THEN json_extract(` + obs + `,'$.facts.aliases') WHEN json_type(` + row + `.metadata_json,'$.source.aliases')='array' THEN json_extract(` + row + `.metadata_json,'$.source.aliases') ELSE '[]' END))) WHERE ` + row + `.authority<>'nexus'`
}

func briefPlanTargetSignature(row string) string {
	fields := []string{}
	for _, field := range []string{"authority", "connection_id", "native_id", "url", "identifier", "identifier_aliases", "aliases", "status", "phase"} {
		fields = append(fields, `json_extract(evidence,'$.`+field+`')`)
	}
	return `json_array(json_extract(` + row + `.metadata_json,'$.source.url'),CASE WHEN ` + row + `.authority='nexus' THEN NULL ELSE COALESCE((SELECT json_extract(body_json,'$.facts.phase') FROM work_observations WHERE id=` + row + `.latest_observation_id),json_extract(` + row + `.metadata_json,'$.phase'),'unknown') END,
 (SELECT json_group_array(signature) FROM (SELECT DISTINCT json_array(` + strings.Join(fields, ",") + `) AS signature FROM (` + briefPlanTargetEntries(row) + `) ORDER BY signature)))`
}

func briefPlanTargetQueue(rows ...string) string {
	entries := []string{}
	direct := []string{}
	for _, row := range rows {
		entries = append(entries, briefPlanTargetEntries(row))
		direct = append(direct, `SELECT 'card:'||`+row+`.card_id AS ref UNION ALL SELECT 'card:'||handle FROM cards WHERE id=`+row+`.card_id UNION ALL SELECT json_extract(`+row+`.metadata_json,'$.source.url')`)
	}
	// Each evidence entry has at most four scalar keys and two bounded alias
	// arrays. Seek the reverse covering index for each spelling, before queuing.
	keys := `SELECT k.value AS ref FROM (` + strings.Join(entries, " UNION ALL ") + `) e CROSS JOIN json_each(json_array(json_extract(evidence,'$.native_id'),json_extract(evidence,'$.authority')||':'||json_extract(evidence,'$.native_id'),json_extract(evidence,'$.url'),json_extract(evidence,'$.identifier'))) k WHERE k.type='text'
 UNION ALL SELECT a.value FROM (` + strings.Join(entries, " UNION ALL ") + `) e CROSS JOIN json_each(evidence,'$.identifier_aliases') a WHERE a.type='text' AND CAST(a.key AS INTEGER)<50
 UNION ALL SELECT a.value FROM (` + strings.Join(entries, " UNION ALL ") + `) e CROSS JOIN json_each(evidence,'$.aliases') a WHERE a.type='text' AND CAST(a.key AS INTEGER)<50`
	return `INSERT INTO brief_dirty SELECT r.card_id FROM (` + strings.Join(direct, " UNION ALL ") + ` UNION ALL ` + keys + `) k CROSS JOIN brief_plan_refs r ON r.ref=k.ref WHERE k.ref<>'' AND length(CAST(k.ref AS BLOB))<=2048 ON CONFLICT DO NOTHING;`
}

func installBriefPlanTargetTriggers(ctx context.Context, tx *sql.Tx) error {
	for _, name := range []string{"metadata_insert", "metadata_update", "metadata_delete", "evidence_insert", "evidence_delete"} {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS brief_plan_target_`+name); err != nil {
			return err
		}
	}
	for _, event := range []struct {
		name, timing, condition string
		rows                    []string
	}{
		{"metadata_insert", "AFTER INSERT", "", []string{"NEW"}},
		{"metadata_update", "AFTER UPDATE OF metadata_json,latest_observation_id,authority,connection_id,native_id", " WHEN " + briefPlanTargetSignature("OLD") + " IS NOT " + briefPlanTargetSignature("NEW"), []string{"OLD", "NEW"}},
		{"metadata_delete", "BEFORE DELETE", "", []string{"OLD"}},
	} {
		q := `CREATE TRIGGER brief_plan_target_` + event.name + ` ` + event.timing + ` ON work_metadata` + event.condition + ` BEGIN ` + briefPlanTargetQueue(event.rows...) + ` END`
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("brief plan target %s: %w", event.name, err)
		}
	}
	return nil
}
