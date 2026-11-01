// Single-purpose index and table installs (migrations 40-49, 86, and 99).

package storage

import (
	"context"
	"database/sql"
)

func applyMigration40HostIdentity(ctx context.Context, tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='auth_invites'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE auth_invites SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE kind<>'human' AND revoked_at IS NULL`)
	return err
}

func applyMigration43ReportIndexes(ctx context.Context, tx *sql.Tx) error {
	// Some legacy partial workspaces omit primitive tables. Follow the existing
	// conditional backfill convention rather than failing their migration.
	for _, item := range []struct{ table, statement string }{
		{"cards", `CREATE INDEX idx_report_cards_board_active ON cards(board_id,id) WHERE archived_at IS NULL AND trashed_at IS NULL`},
		{"work_metadata", `CREATE INDEX idx_work_metadata_project ON work_metadata(json_extract(metadata_json,'$.project_ref'),card_id)`},
	} {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, item.table).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			if _, err := tx.ExecContext(ctx, item.statement); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyMigration44CardPlans(ctx context.Context, tx *sql.Tx) error {
	// Legacy partial workspaces follow the same conditional migration convention
	// as report indexes. Normal workspaces always have both primitive tables.
	for _, item := range []struct{ table, statement string }{
		{"cards", `CREATE TABLE card_plans (card_id TEXT PRIMARY KEY REFERENCES cards(id) ON DELETE CASCADE, body_json TEXT NOT NULL, updated_at TEXT NOT NULL);`},
		{"work_metadata", `CREATE INDEX idx_work_source_url ON work_metadata(json_extract(metadata_json,'$.source.url'));`},
	} {
		exists, err := sqliteTableExists(ctx, tx, item.table)
		if err != nil {
			return err
		}
		if exists {
			if _, err = tx.ExecContext(ctx, item.statement); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyMigration47HumanAttentionResolutions(ctx context.Context, tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at)
		SELECT ask.id, answer.id, 'answered', answer.actor_id, answer.ts
		FROM events AS answer
		JOIN events AS ask ON ask.type='human_attention_requested' AND (
			json_extract(answer.payload_json,'$.payload.request_event_id')=ask.id
			OR json_extract(answer.payload_json,'$.payload.request_event_ref')='event:' || ask.id
			OR json_extract(answer.payload_json,'$.payload.request_event_ref')='event:' || ask.handle
		)
		WHERE answer.type='human_attention_responded';`)
	return err
}

func applyMigration48HumanAttentionAnswerReads(ctx context.Context, tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agent_wakeups'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_answer_reads(answer_event_id,requester_actor_id,read_at)
		SELECT response.id, wake.target_actor_id, COALESCE(wake.read_at,wake.updated_at)
		FROM agent_wakeups AS wake
		JOIN events AS response ON response.type='human_attention_responded'
			AND trim(COALESCE(json_extract(response.payload_json,'$.payload.requester_actor_id'),''))=wake.target_actor_id
		WHERE wake.notification_status='read'
		  AND EXISTS (SELECT 1 FROM json_each(wake.refs_json) AS ref WHERE ref.value='event:' || response.id);`)
	return err

}

func applyMigration49SeriesTokenScope(ctx context.Context, tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='auth_access_tokens'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	} // Legacy partial workspace migrations omit auth tables.
	_, err := tx.ExecContext(ctx, `ALTER TABLE auth_access_tokens ADD COLUMN series_adapter TEXT REFERENCES series_adapters(name)`)
	return err
}

func installAskRequesterIndex(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "events")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_ask_requester ON events(trim(COALESCE(json_extract(payload_json,'$.payload.requester_actor_id'),'')), ts DESC, id DESC) WHERE type='human_attention_requested' AND COALESCE(trashed_at,'')=''`)
	return err
}

func installRunReceiptIndexes(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "events")
	if err != nil || !exists {
		return err
	}
	for _, statement := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_events_run_identity ON events(json_extract(payload_json,'$.subject_ref'),json_extract(payload_json,'$.payload.run_id')) WHERE type='run_recorded'`,
		`CREATE INDEX IF NOT EXISTS idx_events_run_subject_ts ON events(json_extract(payload_json,'$.subject_ref'),ts DESC,id DESC) WHERE type='run_recorded'`,
		`CREATE INDEX IF NOT EXISTS idx_events_run_effect_ts ON events(json_extract(payload_json,'$.subject_ref'),ts DESC,id DESC) WHERE type='run_recorded' AND (json_extract(payload_json,'$.payload.outcome')='acted' OR json_extract(payload_json,'$.payload.experiment.decision')='kept')`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
