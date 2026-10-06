package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// The index is a rebuildable projection, updated in the canonical write's
// transaction. Reads look up exact keys; no workspace metadata scan is needed.
func applyMigration62EvidenceIndex(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "work_metadata")
	if err != nil || !exists {
		return err
	}
	cardsExist, err := sqliteTableExists(ctx, tx, "cards")
	if err != nil {
		return err
	}
	if cardsExist {
		if _, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_cards_read_thread ON cards(COALESCE(NULLIF(trim(thread_id),''),trim(parent_thread_id)))`); err != nil {
			return err
		}
	}
	statements := []string{
		`DROP INDEX IF EXISTS idx_work_source_url;`,
		`CREATE INDEX idx_work_source_url ON work_metadata(json_extract(metadata_json,'$.source.url'),card_id);`,

		`DROP TRIGGER IF EXISTS work_evidence_metadata_insert;`,
		`DROP TRIGGER IF EXISTS work_evidence_metadata_update;`,
		`DROP TRIGGER IF EXISTS work_evidence_metadata_delete;`,
		`DROP VIEW IF EXISTS work_evidence_keys;`,
		`DROP VIEW IF EXISTS work_evidence_entries;`,
		`DROP TABLE IF EXISTS work_evidence_index;`,
		`DROP TABLE IF EXISTS work_evidence_records;`,
		`CREATE TABLE work_evidence_records(id INTEGER PRIMARY KEY,card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,slot TEXT NOT NULL,evidence_json TEXT NOT NULL,UNIQUE(card_id,slot));`,
		`CREATE TABLE work_evidence_index(id INTEGER PRIMARY KEY,card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,lookup_key TEXT NOT NULL,evidence_id INTEGER NOT NULL REFERENCES work_evidence_records(id) ON DELETE CASCADE,UNIQUE(evidence_id,lookup_key));`,
		`CREATE INDEX idx_work_evidence_lookup ON work_evidence_index(lookup_key,id);`,
		`CREATE INDEX idx_work_evidence_public_lookup ON work_evidence_index(lookup_key COLLATE NOCASE,id);`,
		`CREATE INDEX idx_work_evidence_card ON work_evidence_index(card_id);`,
		`CREATE VIEW work_evidence_entries AS
 SELECT m.card_id,'refs:'||j.key AS slot,j.value AS evidence_json FROM work_metadata m,json_each(m.metadata_json,'$.source_refs') j WHERE j.type='object' AND CAST(j.key AS INTEGER)<2000
 UNION ALL SELECT m.card_id,'obs:'||j.key,json_patch(j.value,json_object('observed_at',COALESCE(NULLIF(json_extract(j.value,'$.observed_at'),''),json_extract(o.body_json,'$.observed_at'),''))) FROM work_metadata m JOIN work_observations o ON o.id=m.latest_observation_id,json_each(o.body_json,'$.evidence') j WHERE j.type='object' AND CAST(j.key AS INTEGER)<2000
 UNION ALL SELECT m.card_id,'source',json_patch(COALESCE(json_extract(m.metadata_json,'$.source'),'{}'),json_object(
 'authority',m.authority,'connection_id',COALESCE(m.connection_id,''),'native_id',COALESCE(m.native_id,''),
 'title',COALESCE(json_extract(o.body_json,'$.facts.title'),json_extract(m.metadata_json,'$.title'),json_extract(m.metadata_json,'$.source.title'),''),
 'status',COALESCE(NULLIF(json_extract(o.body_json,'$.facts.native_status'),''),json_extract(o.body_json,'$.facts.phase'),''),
 'phase',COALESCE(json_extract(o.body_json,'$.facts.phase'),''),
 'identifier',COALESCE(json_extract(o.body_json,'$.facts.identifier'),json_extract(m.metadata_json,'$.source.identifier'),''),
 'identifier_aliases',json(CASE WHEN json_type(o.body_json,'$.facts.identifier_aliases')='array' THEN json_extract(o.body_json,'$.facts.identifier_aliases') WHEN json_type(m.metadata_json,'$.source.identifier_aliases')='array' THEN json_extract(m.metadata_json,'$.source.identifier_aliases') ELSE '[]' END),
 'aliases',json(CASE WHEN json_type(o.body_json,'$.facts.aliases')='array' THEN json_extract(o.body_json,'$.facts.aliases') WHEN json_type(m.metadata_json,'$.source.aliases')='array' THEN json_extract(m.metadata_json,'$.source.aliases') ELSE '[]' END),
 'observed_at',COALESCE(json_extract(o.body_json,'$.observed_at'),''),
 'source_activity_at',COALESCE(CASE WHEN julianday(json_extract(o.body_json,'$.source_activity_at'))>julianday(json_extract(o.body_json,'$.meaningful_progress_at')) THEN json_extract(o.body_json,'$.source_activity_at') END,json_extract(o.body_json,'$.meaningful_progress_at'),json_extract(o.body_json,'$.source_activity_at'),'')))
 FROM work_metadata m LEFT JOIN work_observations o ON o.id=m.latest_observation_id WHERE m.authority<>'nexus';`,
		`CREATE VIEW work_evidence_keys AS
 SELECT DISTINCT e.card_id,e.id AS evidence_id,k.value AS lookup_key FROM work_evidence_records e,json_each(json_array(
 json_extract(e.evidence_json,'$.native_id'),json_extract(e.evidence_json,'$.authority')||':'||json_extract(e.evidence_json,'$.native_id'),json_extract(e.evidence_json,'$.url'),json_extract(e.evidence_json,'$.identifier'))) k WHERE k.type='text' AND length(trim(k.value))>0 AND length(CAST(k.value AS BLOB))<=2048
 UNION SELECT e.card_id,e.id,k.value FROM work_evidence_records e,json_each(e.evidence_json,'$.identifier_aliases') k WHERE json_type(e.evidence_json,'$.identifier_aliases')='array' AND CAST(k.key AS INTEGER)<50 AND k.type='text' AND length(trim(k.value))>0 AND length(CAST(k.value AS BLOB))<=2048
 UNION SELECT e.card_id,e.id,k.value FROM work_evidence_records e,json_each(e.evidence_json,'$.aliases') k WHERE json_type(e.evidence_json,'$.aliases')='array' AND CAST(k.key AS INTEGER)<50 AND k.type='text' AND length(trim(k.value))>0 AND length(CAST(k.value AS BLOB))<=2048;`,
	}
	for _, q := range statements {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("evidence index: %w", err)
		}
	}
	// Backfill bounded card batches; no alias row duplicates the evidence JSON.
	cursor := ""
	for {
		rows, err := tx.QueryContext(ctx, `SELECT card_id FROM work_metadata WHERE card_id>? ORDER BY card_id LIMIT 32`, cursor)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		encoded, _ := json.Marshal(ids)
		for _, q := range []string{
			`INSERT INTO work_evidence_records(card_id,slot,evidence_json) SELECT card_id,slot,evidence_json FROM work_evidence_entries WHERE card_id IN (SELECT value FROM json_each(?))`,
			`INSERT INTO work_evidence_index(card_id,lookup_key,evidence_id) SELECT card_id,lookup_key,evidence_id FROM work_evidence_keys WHERE card_id IN (SELECT value FROM json_each(?))`,
		} {
			if _, err = tx.ExecContext(ctx, q, string(encoded)); err != nil {
				return fmt.Errorf("evidence backfill: %w", err)
			}
		}
		cursor = ids[len(ids)-1]
	}
	for _, event := range []string{"INSERT", "UPDATE OF metadata_json,latest_observation_id,authority,connection_id,native_id,updated_at"} {
		name := "insert"
		if event != "INSERT" {
			name = "update"
		}
		q := `CREATE TRIGGER work_evidence_metadata_` + name + ` AFTER ` + event + ` ON work_metadata BEGIN
 DELETE FROM work_evidence_index WHERE card_id=NEW.card_id;
 DELETE FROM work_evidence_records WHERE card_id=NEW.card_id;
 INSERT INTO work_evidence_records(card_id,slot,evidence_json) SELECT card_id,slot,evidence_json FROM work_evidence_entries WHERE card_id=NEW.card_id;
 INSERT INTO work_evidence_index(card_id,lookup_key,evidence_id) SELECT card_id,lookup_key,evidence_id FROM work_evidence_keys WHERE card_id=NEW.card_id; END;`
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE TRIGGER work_evidence_metadata_delete AFTER DELETE ON work_metadata BEGIN DELETE FROM work_evidence_index WHERE card_id=OLD.card_id; DELETE FROM work_evidence_records WHERE card_id=OLD.card_id; END;`)
	if err != nil {
		return err
	}
	// These projections inherit canonical card ownership and every reference in
	// their payload/key. Install atomic edges after the bounded backfill too.
	return installResourceAccessEdges(ctx, tx)
}
