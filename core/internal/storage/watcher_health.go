package storage

import (
	"context"
	"database/sql"
)

// Clock state and compact traversal positions only. Read candidates are
// hydrated through scoped canonical relations before making any health claim.
func installWatcherHealth(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS watcher_health(alarm_key TEXT PRIMARY KEY,card_id TEXT NOT NULL DEFAULT '',unhealthy_since TEXT NOT NULL DEFAULT '',episode INTEGER NOT NULL DEFAULT 0,level TEXT NOT NULL DEFAULT '',recipient TEXT NOT NULL DEFAULT '',ask_id TEXT NOT NULL DEFAULT '',raise_count INTEGER NOT NULL DEFAULT 0,next_raise_at TEXT NOT NULL DEFAULT '',thread_id TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_watcher_health_card ON watcher_health(card_id) WHERE card_id<>''`,
		`CREATE TABLE IF NOT EXISTS watcher_health_cursor(singleton INTEGER PRIMARY KEY CHECK(singleton=1),after_card_id TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO watcher_health_cursor(singleton) VALUES(1) ON CONFLICT DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS watcher_loop_candidates(card_id TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS watcher_receipt_positions(id TEXT PRIMARY KEY,subject_ref TEXT NOT NULL,ts TEXT NOT NULL,is_effect INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_watcher_receipt_subject ON watcher_receipt_positions(subject_ref,ts DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_watcher_receipt_effect ON watcher_receipt_positions(subject_ref,ts DESC,id DESC) WHERE is_effect=1`,
		// Reconcile previews of this unreleased migration without retaining the
		// costly expression indexes or resetting durable episode state.
		`DROP VIEW IF EXISTS watcher_run_positions`, `DROP VIEW IF EXISTS watcher_effect_positions`,
		`DROP INDEX IF EXISTS idx_work_metadata_health`, `DROP INDEX IF EXISTS idx_watcher_loop_runs`, `DROP INDEX IF EXISTS idx_watcher_loop_effects`,
		`CREATE VIEW watcher_run_positions AS SELECT id,ts,subject_ref FROM watcher_receipt_positions`,
		`CREATE VIEW watcher_effect_positions AS SELECT id,ts,subject_ref FROM watcher_receipt_positions WHERE is_effect=1`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	has, err := sqliteTableHasColumn(ctx, tx, "watcher_health", "thread_id")
	if err != nil {
		return err
	}
	if !has {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE watcher_health ADD COLUMN thread_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	annotated := func(row string) string {
		return `(json_type(` + row + `.metadata_json,'$.cadence')='object' OR json_type(` + row + `.metadata_json,'$.goal')='object')`
	}
	receipt := func(row string) string {
		return `INSERT INTO watcher_receipt_positions(id,subject_ref,ts,is_effect) SELECT ` + row + `.id,json_extract(` + row + `.payload_json,'$.subject_ref'),` + row + `.ts,COALESCE(json_extract(` + row + `.payload_json,'$.payload.outcome')='acted' OR json_extract(` + row + `.payload_json,'$.payload.experiment.decision')='kept',0) WHERE ` + row + `.type='run_recorded' AND COALESCE(` + row + `.trashed_at,'')='' AND COALESCE(` + row + `.archived_at,'')='' AND json_extract(` + row + `.payload_json,'$.subject_ref') IS NOT NULL ON CONFLICT(id) DO UPDATE SET subject_ref=excluded.subject_ref,ts=excluded.ts,is_effect=excluded.is_effect;`
	}
	for _, q := range []string{
		`CREATE TRIGGER IF NOT EXISTS watcher_metadata_insert AFTER INSERT ON work_metadata WHEN ` + annotated("NEW") + ` BEGIN INSERT INTO watcher_loop_candidates VALUES(NEW.card_id) ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS watcher_metadata_update AFTER UPDATE OF metadata_json,card_id ON work_metadata BEGIN DELETE FROM watcher_loop_candidates WHERE card_id=OLD.card_id; INSERT INTO watcher_loop_candidates SELECT NEW.card_id WHERE ` + annotated("NEW") + ` ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS watcher_metadata_delete AFTER DELETE ON work_metadata BEGIN DELETE FROM watcher_loop_candidates WHERE card_id=OLD.card_id; END`,
		`CREATE TRIGGER IF NOT EXISTS watcher_receipt_insert AFTER INSERT ON events WHEN NEW.type='run_recorded' BEGIN ` + receipt("NEW") + ` END`,
		`CREATE TRIGGER IF NOT EXISTS watcher_receipt_update AFTER UPDATE OF payload_json,type,ts,archived_at,trashed_at ON events WHEN OLD.type='run_recorded' OR NEW.type='run_recorded' BEGIN DELETE FROM watcher_receipt_positions WHERE id=OLD.id; ` + receipt("NEW") + ` END`,
		`CREATE TRIGGER IF NOT EXISTS watcher_receipt_delete AFTER DELETE ON events WHEN OLD.type='run_recorded' BEGIN DELETE FROM watcher_receipt_positions WHERE id=OLD.id; END`,
		// Existing partial indexes admit only annotated loops / run receipts.
		// No new index traverses the whole events or metadata relation at boot.
		`INSERT INTO watcher_loop_candidates SELECT card_id FROM brief_initiatives INDEXED BY idx_brief_loop_annotated_recent WHERE loop_annotated=1 ON CONFLICT DO NOTHING`,
		`INSERT INTO watcher_receipt_positions(id,subject_ref,ts,is_effect) SELECT id,json_extract(payload_json,'$.subject_ref'),ts,COALESCE(json_extract(payload_json,'$.payload.outcome')='acted' OR json_extract(payload_json,'$.payload.experiment.decision')='kept',0) FROM events INDEXED BY idx_events_run_subject_ts WHERE type='run_recorded' AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' AND json_extract(payload_json,'$.subject_ref') IS NOT NULL ON CONFLICT(id) DO NOTHING`,
	} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
