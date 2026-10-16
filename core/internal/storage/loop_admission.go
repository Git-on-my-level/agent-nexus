package storage

import (
	"context"
	"database/sql"
)

// Admission hints live beside the existing brief projection and update in the
// canonical write transaction, before asynchronous receipt folding. They are
// only index candidates: readers must verify scoped metadata/receipts before
// limiting or returning cards. In particular a private receipt is not authority
// to admit its otherwise public card to a reader's report.
func installLoopAdmission(ctx context.Context, tx *sql.Tx) error {
	// Historical reduced schemas may lack brief projection prerequisites, as
	// with the installer for migration 100. Leave those schemas untouched.
	for _, table := range []string{"brief_initiatives", "cards", "work_metadata", "events"} {
		has, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !has {
			return err
		}
	}
	for _, column := range []struct{ name, definition string }{
		{"loop_annotated", "INTEGER NOT NULL DEFAULT 0"},
		{"loop_last_run_at", "TEXT"},
	} {
		has, err := sqliteTableHasColumn(ctx, tx, "brief_initiatives", column.name)
		if err != nil {
			return err
		}
		if !has {
			if _, err = tx.ExecContext(ctx, `ALTER TABLE brief_initiatives ADD COLUMN `+column.name+` `+column.definition); err != nil {
				return err
			}
		}
	}
	annotated := func(row string) string {
		return `(json_type(` + row + `.metadata_json,'$.cadence')='object' OR json_type(` + row + `.metadata_json,'$.goal')='object')`
	}
	receipt := func(row string) string {
		subject := `json_extract(` + row + `.payload_json,'$.subject_ref')`
		return `INSERT INTO brief_initiatives(card_id,loop_last_run_at)
   SELECT id,(SELECT ts FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=` + subject + ` AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts DESC,id DESC LIMIT 1)
   FROM cards WHERE ` + row + `.type='run_recorded' AND id=substr(` + subject + `,6) AND substr(` + subject + `,1,5)='card:'
   ON CONFLICT(card_id) DO UPDATE SET loop_last_run_at=excluded.loop_last_run_at;`
	}
	for _, query := range []string{
		`CREATE INDEX IF NOT EXISTS idx_brief_loop_annotated ON brief_initiatives(card_id) WHERE loop_annotated=1`,
		`CREATE INDEX IF NOT EXISTS idx_brief_loop_recent ON brief_initiatives(loop_last_run_at DESC,card_id) WHERE loop_last_run_at IS NOT NULL`,
		`CREATE TRIGGER IF NOT EXISTS loop_admission_metadata_insert AFTER INSERT ON work_metadata BEGIN INSERT INTO brief_initiatives(card_id,loop_annotated) VALUES(NEW.card_id,COALESCE(` + annotated("NEW") + `,0)) ON CONFLICT(card_id) DO UPDATE SET loop_annotated=excluded.loop_annotated; END`,
		`CREATE TRIGGER IF NOT EXISTS loop_admission_metadata_update AFTER UPDATE OF metadata_json ON work_metadata BEGIN UPDATE brief_initiatives SET loop_annotated=COALESCE(` + annotated("NEW") + `,0) WHERE card_id=NEW.card_id; END`,
		`CREATE TRIGGER IF NOT EXISTS loop_admission_metadata_delete AFTER DELETE ON work_metadata BEGIN UPDATE brief_initiatives SET loop_annotated=0 WHERE card_id=OLD.card_id; END`,
		`CREATE TRIGGER IF NOT EXISTS loop_admission_event_insert AFTER INSERT ON events WHEN NEW.type='run_recorded' BEGIN ` + receipt("NEW") + ` END`,
		`CREATE TRIGGER IF NOT EXISTS loop_admission_event_update AFTER UPDATE OF payload_json,type,ts,archived_at,trashed_at ON events WHEN OLD.type='run_recorded' OR NEW.type='run_recorded' BEGIN ` + receipt("OLD") + receipt("NEW") + ` END`,
		`CREATE TRIGGER IF NOT EXISTS loop_admission_event_delete AFTER DELETE ON events WHEN OLD.type='run_recorded' BEGIN ` + receipt("OLD") + ` END`,
		`INSERT INTO brief_initiatives(card_id,loop_annotated,loop_last_run_at)
   SELECT c.id,COALESCE((SELECT ` + annotated("m") + ` FROM work_metadata m WHERE m.card_id=c.id),0),
    (SELECT ts FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')='card:'||c.id AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts DESC,id DESC LIMIT 1)
   FROM cards c WHERE true ON CONFLICT(card_id) DO UPDATE SET loop_annotated=excluded.loop_annotated,loop_last_run_at=excluded.loop_last_run_at`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
