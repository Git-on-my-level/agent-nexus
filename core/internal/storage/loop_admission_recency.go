package storage

import (
	"context"
	"database/sql"
)

// Annotation recency is captured on the same write as the annotation. Do not use
// computed_at: asynchronous fold order is unrelated to authored activity.
func installLoopAdmissionRecency(ctx context.Context, tx *sql.Tx) error {
	has, err := sqliteTableHasColumn(ctx, tx, "brief_initiatives", "loop_annotated")
	if err != nil || !has {
		return err
	}
	has, err = sqliteTableHasColumn(ctx, tx, "brief_initiatives", "loop_annotated_at")
	if err != nil {
		return err
	}
	if !has {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE brief_initiatives ADD COLUMN loop_annotated_at TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	for _, query := range []string{
		`DROP INDEX IF EXISTS idx_brief_loop_annotated`,
		`CREATE INDEX IF NOT EXISTS idx_brief_loop_annotated_recent ON brief_initiatives(loop_annotated_at DESC,card_id) WHERE loop_annotated=1`,
		`DROP TRIGGER IF EXISTS loop_admission_metadata_insert`,
		`DROP TRIGGER IF EXISTS loop_admission_metadata_update`,
		`DROP TRIGGER IF EXISTS loop_admission_metadata_delete`,
		`CREATE TRIGGER loop_admission_metadata_insert AFTER INSERT ON work_metadata BEGIN INSERT INTO brief_initiatives(card_id,loop_annotated,loop_annotated_at) VALUES(NEW.card_id,COALESCE(json_type(NEW.metadata_json,'$.cadence')='object' OR json_type(NEW.metadata_json,'$.goal')='object',0),NEW.updated_at) ON CONFLICT(card_id) DO UPDATE SET loop_annotated=excluded.loop_annotated,loop_annotated_at=excluded.loop_annotated_at; END`,
		`CREATE TRIGGER loop_admission_metadata_update AFTER UPDATE OF metadata_json,updated_at ON work_metadata BEGIN UPDATE brief_initiatives SET loop_annotated=COALESCE(json_type(NEW.metadata_json,'$.cadence')='object' OR json_type(NEW.metadata_json,'$.goal')='object',0),loop_annotated_at=NEW.updated_at WHERE card_id=NEW.card_id; END`,
		`CREATE TRIGGER loop_admission_metadata_delete AFTER DELETE ON work_metadata BEGIN UPDATE brief_initiatives SET loop_annotated=0,loop_annotated_at='' WHERE card_id=OLD.card_id; END`,
		`UPDATE brief_initiatives SET loop_annotated_at=COALESCE((SELECT m.updated_at FROM work_metadata m WHERE m.card_id=brief_initiatives.card_id),'') WHERE loop_annotated=1`,
	} {
		if _, err = tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
