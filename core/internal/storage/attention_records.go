package storage

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
)

// Placement records are canonical; their authored payload is an indexed filter
// ownership source. Position views expose traversal metadata only; content
// always loads through scoped records or canonical events.
func installAttentionRecords(ctx context.Context, tx *sql.Tx) error {
	hasScope, err := sqliteTableHasColumn(ctx, tx, "overview_visits", "scope_ref")
	if err != nil {
		return err
	}
	if !hasScope {
		for _, q := range []string{
			`ALTER TABLE overview_visits RENAME TO overview_visits_legacy`,
			`CREATE TABLE overview_visits(principal_id TEXT NOT NULL,scope_ref TEXT NOT NULL DEFAULT 'builtin:overview',visited_at TEXT NOT NULL,snapshot_json TEXT NOT NULL,PRIMARY KEY(principal_id,scope_ref))`,
			`INSERT INTO overview_visits(principal_id,visited_at,snapshot_json) SELECT principal_id,visited_at,snapshot_json FROM overview_visits_legacy`,
			`DROP TABLE overview_visits_legacy`,
		} {
			if _, err = tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS attention_settings(singleton INTEGER PRIMARY KEY CHECK(singleton=1),attention_agent TEXT,updated_by TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS attention_subjects(for_actor TEXT NOT NULL,subject_ref TEXT NOT NULL,PRIMARY KEY(for_actor,subject_ref))`,
		`CREATE TABLE IF NOT EXISTS attention_placements(for_actor TEXT NOT NULL,subject_ref TEXT NOT NULL,placed_by TEXT NOT NULL,record_id TEXT NOT NULL UNIQUE,placed_at TEXT NOT NULL,payload_json TEXT NOT NULL,PRIMARY KEY(for_actor,subject_ref,placed_by))`,
		`CREATE INDEX IF NOT EXISTS idx_attention_placements_latest ON attention_placements(for_actor,subject_ref,placed_at DESC,record_id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_attention_feedback_page ON events(ts DESC,id DESC) WHERE type='attention_feedback'`,
		`CREATE INDEX IF NOT EXISTS idx_attention_feedback_for_page ON events(json_extract(payload_json,'$.payload.for'),ts DESC,id DESC) WHERE type='attention_feedback'`,
		`CREATE INDEX IF NOT EXISTS idx_attention_asks_recipient ON brief_asks(recipient,state,ask_id)`,
		`CREATE VIEW IF NOT EXISTS attention_placement_positions AS SELECT for_actor,subject_ref,placed_by,record_id,placed_at FROM attention_placements`,
		`CREATE VIEW IF NOT EXISTS attention_ask_positions AS SELECT recipient,state,ask_id FROM brief_asks`,
		`CREATE VIEW IF NOT EXISTS attention_feedback_positions AS SELECT id,ts,type,json_extract(payload_json,'$.payload.for') AS for_ref FROM events WHERE type='attention_feedback'`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return installResourceAccessSourceEdges(ctx, tx, []resourceaccess.OwnershipSource{resourceaccess.AttentionPlacementSource})
}
