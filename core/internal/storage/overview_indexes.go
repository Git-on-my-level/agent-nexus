package storage

import (
	_ "agent-nexus-core/internal/sqltext"
	"context"
	"database/sql"
	"strings"
)

// Indexed selectors and bounded projection ordering; no new read-model state.
func indexBoundedOverviewReads(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_inbox_category_page ON derived_inbox_items(CASE anx_unicode_trim(category) WHEN 'escalate' THEN 0 WHEN 'ask' THEN 1 WHEN 'review' THEN 2 ELSE 99 END,trigger_at DESC,id ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_boards_thread_id ON boards(thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cards_thread_id ON cards(thread_id)`,

		`CREATE INDEX IF NOT EXISTS idx_cards_work_page ON cards(trashed_at,archived_at,anx_timestamp_key(updated_at) DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_dashboard ON documents(updated_at DESC,id ASC) WHERE COALESCE(archived_at,'')='' AND COALESCE(trashed_at,'')=''`,
		`CREATE INDEX IF NOT EXISTS idx_host_keys_latest ON host_keys(host_id,created_at DESC,id)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_admin_page ON agents(username,id) WHERE revoked_at IS NULL AND COALESCE(json_extract(metadata_json,'$.auth_admin'),0)=1`,
		`CREATE INDEX IF NOT EXISTS idx_runs_agent_date ON runs(agent_id,anx_timestamp_key(last_observed_at) DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_agent_active_date ON runs(agent_id,anx_timestamp_key(last_observed_at) DESC,id DESC) WHERE state NOT IN ('completed','failed','cancelled') AND liveness='alive'`,
		`CREATE INDEX IF NOT EXISTS idx_events_actor_date ON events(actor_id,anx_timestamp_key(ts) DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_ask_order ON events(type,anx_timestamp_key(ts),id) WHERE trashed_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_events_request_id ON events(type,json_extract(payload_json,'$.payload.request_event_id')) WHERE trashed_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_events_request_ref ON events(type,json_extract(payload_json,'$.payload.request_event_ref')) WHERE trashed_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_agents_actor ON agents(actor_id,created_at,id)`,
		`CREATE INDEX IF NOT EXISTS idx_actors_display_lower ON actors(anx_unicode_lower(display_name))`,
		`CREATE INDEX IF NOT EXISTS idx_agents_username_lower ON agents(anx_unicode_lower(username))`,
	}
	for _, q := range statements {
		table := strings.Split(strings.SplitN(q, " ON ", 2)[1], "(")[0]
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
