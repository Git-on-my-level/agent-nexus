// Package contentusage owns the canonical accounting input inventory.
package contentusage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Tables excludes rebuildable indexes/projections, even when they hold text.
func Tables() []string {
	return []string{
		"events", "threads", "topics", "ref_edges", "agent_wakeups", "artifacts", "documents", "document_revisions",
		"boards", "cards", "card_revisions", "card_plans", "work_metadata", "work_observations",
		"runs", "agent_presence", "agent_progress_notes", "agent_sessions", "work_participants",
		"actors", "agents", "hosts", "secrets", "workspace_dashboard", "pm_records",
		"observation_investigation_specs", "series_adapters", "series_definitions",
		"series_labels", "series_points", "series_daily",
	}
}

func TrackingTables() []string { return append(Tables(), "blob_usage_totals") }

type Database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Install(ctx context.Context, tx Database) error {
	for _, table := range TrackingTables() {
		if err := InstallTable(ctx, tx, table); err != nil {
			return err
		}
	}
	return nil
}

// InstallTable also supports optional modules initialized after core migrations.
// No canonical row is scanned or rewritten; only a scalar dirty revision is kept.
func InstallTable(ctx context.Context, tx Database, table string) error {
	allowed := false
	for _, name := range TrackingTables() {
		allowed = allowed || name == table
	}
	if !allowed {
		return fmt.Errorf("unsupported accounting table")
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS background_usage_revision(singleton INTEGER PRIMARY KEY CHECK(singleton=1),revision INTEGER NOT NULL);INSERT INTO background_usage_revision VALUES(1,0) ON CONFLICT DO NOTHING;`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_xinfo(?) WHERE hidden=0 ORDER BY cid`, table)
	if err != nil {
		return err
	}
	var cols, changed []string
	for rows.Next() {
		var col string
		if err = rows.Scan(&col); err != nil {
			rows.Close()
			return err
		}
		if strings.HasPrefix(col, "filter_") || col == "search_text" || col == "content_refs_json" {
			continue
		}
		quoted := `"` + strings.ReplaceAll(col, `"`, `""`) + `"`
		cols = append(cols, quoted)
		changed = append(changed, "OLD."+quoted+" IS NOT NEW."+quoted)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(cols) == 0 {
		return err
	}
	for _, op := range []string{"insert", "delete", "update"} {
		event := strings.ToUpper(op)
		if op == "update" {
			event += " OF " + strings.Join(cols, ",")
		}
		guard := ""
		if op == "update" {
			guard = " WHEN " + strings.Join(changed, " OR ")
		}
		if _, err = tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS background_usage_`+table+`_`+op); err != nil {
			return err
		}
		q := `CREATE TRIGGER IF NOT EXISTS background_usage_` + table + `_` + op + ` AFTER ` + event + ` ON "` + table + `"` + guard + ` BEGIN UPDATE background_usage_revision SET revision=revision+1 WHERE singleton=1;END`
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
