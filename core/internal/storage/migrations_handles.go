// Migration 25: backfilling resource handles across tables with stable
// unique handle generation.

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"agent-nexus-core/internal/handles"
)

func applyMigration25ResourceHandles(ctx context.Context, tx *sql.Tx) error {
	specs := []struct {
		table        string
		typ          string
		titleColumns []string
		orderColumns []string
		indexName    string
	}{
		{"topics", "topic", []string{"title"}, []string{"created_at", "id"}, "idx_topics_handle_unique"},
		{"boards", "board", []string{"title"}, []string{"created_at", "id"}, "idx_boards_handle_unique"},
		{"cards", "card", []string{"title"}, []string{"created_at", "id"}, "idx_cards_handle_unique"},
		{"documents", "document", []string{"slug", "title"}, []string{"created_at", "id"}, "idx_documents_handle_unique"},
		{"artifacts", "artifact", []string{"kind"}, []string{"created_at", "id"}, "idx_artifacts_handle_unique"},
		{"events", "event", []string{"type"}, []string{"ts", "id"}, "idx_events_handle_unique"},
		{"threads", "thread", []string{"body_json"}, []string{"updated_at", "id"}, "idx_threads_handle_unique"},
	}
	for _, spec := range specs {
		ok, err := sqliteTableExists(ctx, tx, spec.table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		has, err := sqliteTableHasColumn(ctx, tx, spec.table, "handle")
		if err != nil {
			return fmt.Errorf("migration 25 pragma %s.handle: %w", spec.table, err)
		}
		if !has {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE `+spec.table+` ADD COLUMN handle TEXT;`); err != nil {
				return fmt.Errorf("migration 25 add %s.handle: %w", spec.table, err)
			}
		}
		titleExpr, orderExpr, err := migration25HandleExpressions(ctx, tx, spec.table, spec.titleColumns, spec.orderColumns)
		if err != nil {
			return err
		}
		if err := migration25BackfillHandles(ctx, tx, spec.table, spec.typ, titleExpr, orderExpr); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS `+spec.indexName+` ON `+spec.table+` (handle) WHERE handle IS NOT NULL AND trim(handle) <> '';`); err != nil {
			return fmt.Errorf("migration 25 create %s: %w", spec.indexName, err)
		}
	}
	return nil
}

func migration25HandleExpressions(ctx context.Context, tx *sql.Tx, table string, titleColumns, orderColumns []string) (string, string, error) {
	titleParts := make([]string, 0, len(titleColumns))
	for _, col := range titleColumns {
		has, err := sqliteTableHasColumn(ctx, tx, table, col)
		if err != nil {
			return "", "", err
		}
		if !has {
			continue
		}
		if table == "threads" && col == "body_json" {
			titleParts = append(titleParts, "json_extract(body_json, '$.title')")
			continue
		}
		titleParts = append(titleParts, "NULLIF("+col+", '')")
	}
	titleExpr := "''"
	if len(titleParts) > 0 {
		titleExpr = strings.Join(titleParts, ", ")
	}
	orderParts := make([]string, 0, len(orderColumns))
	for _, col := range orderColumns {
		has, err := sqliteTableHasColumn(ctx, tx, table, col)
		if err != nil {
			return "", "", err
		}
		if has {
			orderParts = append(orderParts, col)
		}
	}
	if len(orderParts) == 0 {
		orderParts = append(orderParts, "id")
	}
	return titleExpr, strings.Join(orderParts, ", "), nil
}

func migration25BackfillHandles(ctx context.Context, tx *sql.Tx, table, typ, titleExpr, orderExpr string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(`+titleExpr+`, '') FROM `+table+` WHERE COALESCE(trim(handle), '') = '' ORDER BY `+orderExpr)
	if err != nil {
		return fmt.Errorf("migration 25 select %s handles: %w", table, err)
	}
	defer rows.Close()
	type row struct{ id, title string }
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title); err != nil {
			return fmt.Errorf("migration 25 scan %s handle: %w", table, err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migration 25 iterate %s handles: %w", table, err)
	}
	used := map[string]struct{}{}
	existing, err := tx.QueryContext(ctx, `SELECT id, handle FROM `+table)
	if err != nil {
		return fmt.Errorf("migration 25 select existing %s identities: %w", table, err)
	}
	for existing.Next() {
		var id string
		var h sql.NullString
		if err := existing.Scan(&id, &h); err != nil {
			_ = existing.Close()
			return err
		}
		if id = strings.TrimSpace(id); id != "" {
			used[id] = struct{}{}
		}
		if h.Valid {
			if handle := strings.TrimSpace(h.String); handle != "" {
				used[handle] = struct{}{}
			}
		}
	}
	if err := existing.Close(); err != nil {
		return err
	}
	for _, r := range pending {
		base := handles.Candidate(r.title, typ+"-"+r.id)
		handle := migration25UniqueHandle(base, used)
		used[handle] = struct{}{}
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET handle = ? WHERE id = ?`, handle, r.id); err != nil {
			return fmt.Errorf("migration 25 update %s handle: %w", table, err)
		}
	}
	return nil
}

func migration25UniqueHandle(base string, used map[string]struct{}) string {
	base = handles.Candidate(base, base)
	for i := 0; ; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", strings.TrimRight(base, "-"), i+1)
			if len(candidate) > handles.MaxLength {
				suffix := fmt.Sprintf("-%d", i+1)
				candidate = strings.TrimRight(base[:handles.MaxLength-len(suffix)], "-") + suffix
			}
		}
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}
