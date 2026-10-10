package storage

import (
	"context"
	"database/sql"
)

// Legacy migration fixtures may contain only a subset of the resource tables.
// Install each routing index/view only after its canonical table exists.
func installPMWorkspaceContext(ctx context.Context, tx *sql.Tx) error {
	for _, family := range []struct {
		table      string
		statements []string
	}{
		{"cards", []string{
			`CREATE INDEX IF NOT EXISTS idx_cards_pm_attention ON cards(CASE column_key WHEN 'blocked' THEN 0 WHEN 'review' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'ready' THEN 3 ELSE 4 END,updated_at DESC,id DESC) WHERE archived_at IS NULL AND trashed_at IS NULL AND column_key NOT IN ('done','cancelled')`,
			`CREATE VIEW IF NOT EXISTS pm_workspace_card_positions AS SELECT id,CASE column_key WHEN 'blocked' THEN 0 WHEN 'review' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'ready' THEN 3 ELSE 4 END AS attention,updated_at FROM cards WHERE archived_at IS NULL AND trashed_at IS NULL AND column_key NOT IN ('done','cancelled')`,
		}},
		{"derived_inbox_items", []string{
			`CREATE INDEX IF NOT EXISTS idx_inbox_pm_recipient ON derived_inbox_items(category,COALESCE(json_extract(data_json,'$.recipient_actor_id'),''),trigger_at,id)`,
			`CREATE VIEW IF NOT EXISTS pm_workspace_ask_positions AS SELECT id,category,COALESCE(json_extract(data_json,'$.recipient_actor_id'),'') AS recipient,trigger_at FROM derived_inbox_items`,
		}},
		{"events", []string{
			`CREATE INDEX IF NOT EXISTS idx_events_pm_recent ON events(ts DESC,id DESC)`,
			`CREATE VIEW IF NOT EXISTS pm_workspace_event_positions AS SELECT id,ts FROM events`,
		}},
	} {
		exists, err := sqliteTableExists(ctx, tx, family.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		for _, statement := range family.statements {
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	return nil
}
