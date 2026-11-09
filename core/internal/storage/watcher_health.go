package storage

import (
	"context"
	"database/sql"
)

// Bookkeeping contains identities and clock values only. Canonical asks and
// placements retain the existing ownership graph. No historical content scan.
func installWatcherHealth(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS watcher_health(alarm_key TEXT PRIMARY KEY,card_id TEXT NOT NULL DEFAULT '',unhealthy_since TEXT NOT NULL DEFAULT '',episode INTEGER NOT NULL DEFAULT 0,level TEXT NOT NULL DEFAULT '',recipient TEXT NOT NULL DEFAULT '',ask_id TEXT NOT NULL DEFAULT '',raise_count INTEGER NOT NULL DEFAULT 0,next_raise_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_watcher_health_card ON watcher_health(card_id) WHERE card_id<>''`,
		`CREATE INDEX IF NOT EXISTS idx_work_metadata_health ON work_metadata(card_id) WHERE json_type(metadata_json,'$.cadence')='object' OR json_type(metadata_json,'$.goal')='object'`,
		`CREATE TABLE IF NOT EXISTS watcher_health_cursor(singleton INTEGER PRIMARY KEY CHECK(singleton=1),after_card_id TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO watcher_health_cursor(singleton) VALUES(1) ON CONFLICT DO NOTHING`,
		`CREATE INDEX IF NOT EXISTS idx_watcher_loop_runs ON events(json_extract(payload_json,'$.subject_ref'),ts DESC,id DESC) WHERE type='run_recorded' AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')=''`,
		`CREATE INDEX IF NOT EXISTS idx_watcher_loop_effects ON events(json_extract(payload_json,'$.subject_ref'),ts DESC,id DESC) WHERE type='run_recorded' AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' AND (json_extract(payload_json,'$.payload.outcome')='acted' OR json_extract(payload_json,'$.payload.experiment.decision')='kept')`,
		`CREATE VIEW IF NOT EXISTS watcher_run_positions AS SELECT id,ts,json_extract(payload_json,'$.subject_ref') AS subject_ref FROM events INDEXED BY idx_watcher_loop_runs WHERE type='run_recorded' AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')=''`,
		`CREATE VIEW IF NOT EXISTS watcher_effect_positions AS SELECT id,ts,json_extract(payload_json,'$.subject_ref') AS subject_ref FROM events INDEXED BY idx_watcher_loop_effects WHERE type='run_recorded' AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' AND (json_extract(payload_json,'$.payload.outcome')='acted' OR json_extract(payload_json,'$.payload.experiment.decision')='kept')`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
