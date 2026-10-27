package storage

import (
	"context"
	"database/sql"
)

func installInitiativeBoardAuthority(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "boards")
	if err != nil || !exists {
		return err
	}
	exists, err = sqliteTableHasColumn(ctx, tx, "boards", "initiative_target")
	if err != nil {
		return err
	}
	if !exists {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE boards ADD COLUMN initiative_target INTEGER CHECK(initiative_target IS NULL OR initiative_target>=0)`); err != nil {
			return err
		}
	}
	// No graph backfill: old board members retain their existing grouping.
	exists, err = sqliteTableHasColumn(ctx, tx, "cards", "initiative_root")
	if err != nil {
		return err
	}
	if !exists {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE cards ADD COLUMN initiative_root INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_events_initiative_changes ON events(anx_timestamp_key(ts) DESC,id DESC) WHERE type IN ('initiative_promoted','initiative_retired','initiative_moved')`,
		`CREATE VIEW IF NOT EXISTS initiative_change_positions AS SELECT id,ts FROM events INDEXED BY idx_events_initiative_changes WHERE type IN ('initiative_promoted','initiative_retired','initiative_moved')`,
		`CREATE VIEW IF NOT EXISTS initiative_legacy_parent_positions AS SELECT c.id FROM cards c WHERE EXISTS(SELECT 1 FROM brief_plan_refs link INDEXED BY idx_brief_plan_refs_target CROSS JOIN cards parent ON parent.id=link.card_id CROSS JOIN boards b ON b.id=parent.board_id WHERE link.ref IN ('card:'||c.id,'card:'||c.handle) AND parent.id<>c.id AND parent.archived_at IS NULL AND parent.trashed_at IS NULL AND parent.column_key NOT IN ('done','cancelled') AND b.archived_at IS NULL AND b.trashed_at IS NULL)`,
		`CREATE VIEW IF NOT EXISTS initiative_card_positions AS SELECT c.id FROM boards b CROSS JOIN cards c INDEXED BY idx_report_cards_board_active ON c.board_id=b.id WHERE b.role='initiatives' AND b.archived_at IS NULL AND b.trashed_at IS NULL AND c.archived_at IS NULL AND c.trashed_at IS NULL AND c.column_key NOT IN ('done','cancelled')`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
