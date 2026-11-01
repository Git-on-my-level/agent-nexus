// Early legacy schema cleanups (migrations 9-17, 22, 24): status and column
// drops plus summary moves across threads, cards, boards, documents, topics,
// and the derived inbox.

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func applyMigration9LegacyStatusCleanup(ctx context.Context, tx *sql.Tx) error {
	execIfTable := func(table, stmt string) error {
		ok, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !ok {
			return err
		}
		_, err = tx.ExecContext(ctx, stmt)
		return err
	}
	stmts := []struct {
		table string
		sql   string
	}{
		{"topics", `UPDATE topics
			    SET archived_at = COALESCE(NULLIF(archived_at, ''), updated_at, created_at),
			        archived_by = COALESCE(NULLIF(archived_by, ''), updated_by, created_by)
			  WHERE status = 'archived'
			    AND COALESCE(NULLIF(archived_at, ''), '') = ''
			    AND COALESCE(NULLIF(trashed_at, ''), '') = '';`},
		{"boards", `UPDATE boards
			    SET archived_at = COALESCE(NULLIF(archived_at, ''), updated_at, created_at),
			        archived_by = COALESCE(NULLIF(archived_by, ''), updated_by, created_by)
			  WHERE status IN ('paused', 'closed')
			    AND COALESCE(NULLIF(archived_at, ''), '') = ''
			    AND COALESCE(NULLIF(trashed_at, ''), '') = '';`},
		{"threads", `UPDATE threads
			    SET archived_at = COALESCE(NULLIF(archived_at, ''), updated_at),
			        archived_by = COALESCE(NULLIF(archived_by, ''), updated_by)
			  WHERE filter_status = 'archived'
			    AND COALESCE(NULLIF(archived_at, ''), '') = ''
			    AND COALESCE(NULLIF(trashed_at, ''), '') = '';`},
		{"cards", `UPDATE cards
			    SET column_key = 'done',
			        resolution = CASE WHEN COALESCE(NULLIF(resolution, ''), '') = '' THEN 'done' ELSE resolution END
			  WHERE status = 'done' AND column_key != 'done';`},
		{"cards", `UPDATE cards
			    SET column_key = 'done',
			        resolution = CASE WHEN COALESCE(NULLIF(resolution, ''), '') = '' THEN 'canceled' ELSE resolution END
			  WHERE status = 'cancelled' AND column_key != 'done';`},
		{"cards", `UPDATE cards SET column_key = 'in_progress' WHERE status = 'in_progress' AND column_key = 'backlog';`},
		{"cards", `UPDATE cards SET column_key = 'ready' WHERE status = 'todo' AND column_key = 'backlog';`},
		{"card_versions", `UPDATE card_versions
			    SET column_key = 'done',
			        resolution = CASE WHEN COALESCE(NULLIF(resolution, ''), '') = '' THEN 'done' ELSE resolution END
			  WHERE status = 'done' AND column_key != 'done';`},
		{"card_versions", `UPDATE card_versions
			    SET column_key = 'done',
			        resolution = CASE WHEN COALESCE(NULLIF(resolution, ''), '') = '' THEN 'canceled' ELSE resolution END
			  WHERE status = 'cancelled' AND column_key != 'done';`},
		{"card_versions", `UPDATE card_versions SET column_key = 'in_progress' WHERE status = 'in_progress' AND column_key = 'backlog';`},
		{"card_versions", `UPDATE card_versions SET column_key = 'ready' WHERE status = 'todo' AND column_key = 'backlog';`},
	}
	for _, s := range stmts {
		if err := execIfTable(s.table, s.sql); err != nil {
			return fmt.Errorf("migration 9 exec on %s: %w", s.table, err)
		}
	}
	for _, d := range []struct {
		table, column string
	}{
		{"topics", "status"},
		{"boards", "status"},
		{"threads", "filter_status"},
		{"cards", "status"},
		{"card_versions", "status"},
	} {
		ok, err := sqliteTableExists(ctx, tx, d.table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		q := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", d.table, d.column)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migration 9 %s: %w", q, err)
		}
	}
	return nil
}

func applyMigration10DropThreadFilterColumns(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "threads")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	stmts := []string{
		`DROP INDEX IF EXISTS idx_threads_priority_updated_at;`,
		`DROP INDEX IF EXISTS idx_threads_cadence_preset_updated_at;`,
		`ALTER TABLE threads DROP COLUMN filter_priority;`,
		`ALTER TABLE threads DROP COLUMN filter_owner;`,
		`ALTER TABLE threads DROP COLUMN filter_due_at;`,
		`ALTER TABLE threads DROP COLUMN filter_cadence;`,
		`ALTER TABLE threads DROP COLUMN filter_cadence_preset;`,
		`ALTER TABLE threads DROP COLUMN filter_tags_json;`,
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migration 10: %w", err)
		}
	}
	return nil
}

func applyMigration11CardsSummaryAndDropPriority(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"cards", "card_versions"} {
		ok, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		hasPriority, err := sqliteTableHasColumn(ctx, tx, table, "priority")
		if err != nil {
			return fmt.Errorf("migration 11 pragma %s.priority: %w", table, err)
		}
		if hasPriority {
			q := fmt.Sprintf("ALTER TABLE %s DROP COLUMN priority;", table)
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("migration 11 %s: %w", q, err)
			}
		}
		hasBodyMarkdown, err := sqliteTableHasColumn(ctx, tx, table, "body_markdown")
		if err != nil {
			return fmt.Errorf("migration 11 pragma %s.body_markdown: %w", table, err)
		}
		if hasBodyMarkdown {
			q := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN body_markdown TO summary;", table)
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("migration 11 %s: %w", q, err)
			}
		}
	}
	return nil
}

// applyMigration14ClearDerivedInboxForCanonicalCategories removes materialized inbox rows so
// projections can rebuild with enums.inbox_category values and slimmer per-row data_json.
func applyMigration14ClearDerivedInboxForCanonicalCategories(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "derived_inbox_items")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_inbox_items`); err != nil {
		return fmt.Errorf("migration 14 clear derived_inbox_items: %w", err)
	}
	threadsOK, err := sqliteTableExists(ctx, tx, "threads")
	if err != nil {
		return err
	}
	if !threadsOK {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO derived_topic_dirty_queue (thread_id, dirty_at)
		SELECT id, strftime('%Y-%m-%dT%H:%M:%fZ', 'now') FROM threads`); err != nil {
		return fmt.Errorf("migration 14 queue derived topic rebuild: %w", err)
	}
	return nil
}

// applyMigration15DropBoardsDocumentsLabelsJSON removes labels_json from boards and documents
// (launch schema simplification; no indexes referenced this column). actors.tags_json is kept:
// it is used by the actors store and auth for identity/credential metadata, not for board/doc labeling.
func applyMigration15DropBoardsDocumentsLabelsJSON(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"boards", "documents"} {
		ok, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		has, err := sqliteTableHasColumn(ctx, tx, table, "labels_json")
		if err != nil {
			return fmt.Errorf("migration 15 pragma %s.labels_json: %w", table, err)
		}
		if !has {
			continue
		}
		q := fmt.Sprintf("ALTER TABLE %s DROP COLUMN labels_json;", table)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migration 15 %s: %w", q, err)
		}
	}
	return nil
}

// applyMigration16DropTopicsTypeColumn removes topic classification; topics are identified by title/summary/refs only.
func applyMigration16DropTopicsTypeColumn(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "topics")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_topics_type_updated_at`); err != nil {
		return fmt.Errorf("migration 16 drop idx_topics_type_updated_at: %w", err)
	}
	has, err := sqliteTableHasColumn(ctx, tx, "topics", "type")
	if err != nil {
		return fmt.Errorf("migration 16 pragma topics.type: %w", err)
	}
	if !has {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE topics DROP COLUMN type`); err != nil {
		return fmt.Errorf("migration 16 drop topics.type: %w", err)
	}
	return nil
}

func applyMigration17BoardsDocumentsSummary(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"boards", "documents"} {
		ok, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		has, err := sqliteTableHasColumn(ctx, tx, table, "summary")
		if err != nil {
			return fmt.Errorf("migration 17 pragma %s.summary: %w", table, err)
		}
		if has {
			continue
		}
		q := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN summary TEXT NOT NULL DEFAULT ''`, table)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migration 17 add %s.summary: %w", table, err)
		}
	}
	return nil
}

func applyMigration22TrashCanceledResolutionCards(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "cards")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `
UPDATE cards SET
  trashed_at = ?,
  trashed_by = 'schema_migration',
  trash_reason = 'migrated_from_canceled_resolution',
  resolution = NULL,
  resolution_refs_json = '[]',
  archived_at = NULL,
  archived_by = NULL
WHERE (LOWER(COALESCE(resolution, '')) IN ('canceled', 'cancelled'))
  AND (trashed_at IS NULL OR TRIM(trashed_at) = '')`,
		now)
	if err != nil {
		return fmt.Errorf("migration 22 trash canceled resolution cards: %w", err)
	}
	return nil
}

func applyMigration24HomeGroupCursors(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "home_topic_read_cursors")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO home_read_cursors (reader_id, group_ref, last_read_event_ts, last_read_event_id, updated_at)
		SELECT reader_id, 'topic:' || topic_id, last_read_event_ts, last_read_event_id, updated_at
		FROM home_topic_read_cursors`)
	if err != nil {
		return fmt.Errorf("migration 24 migrate topic cursors: %w", err)
	}
	_, err = tx.ExecContext(ctx, `DROP TABLE IF EXISTS home_topic_read_cursors`)
	if err != nil {
		return fmt.Errorf("migration 24 drop old table: %w", err)
	}
	return nil
}

func nullableMigrationString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
