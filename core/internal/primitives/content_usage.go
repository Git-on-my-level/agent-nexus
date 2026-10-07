package primitives

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Bill canonical row values, not SQLite allocation, indexes, WAL, FTS,
// authorization graphs or projections. Add new canonical content tables here;
// the allowlist prevents accidentally charging for new internal tables.
var contentUsageTables = []string{
	"events", "threads", "topics", "ref_edges", "agent_wakeups", "artifacts", "documents", "document_revisions",
	"boards", "cards", "card_revisions", "card_plans", "work_metadata", "work_observations",
	"runs", "agent_presence", "agent_progress_notes", "agent_sessions", "work_participants",
	"actors", "agents", "hosts", "secrets", "workspace_dashboard", "pm_records",
	"observation_investigation_specs", "series_adapters", "series_definitions",
	"series_labels", "series_points", "series_daily",
}

const contentUsageCacheTTL = 30 * time.Second

func (s *Store) databaseContentUsageBytes(ctx context.Context) (int64, error) {
	// Preserve blob-only behavior for stores without database accounting.
	if strings.TrimSpace(s.dbPath) == "" {
		return 0, nil
	}
	if _, scoped := accessScopeFrom(ctx); scoped {
		// Never share canonical totals with readers or cache authorization decisions.
		return s.measureDatabaseContentUsageBytes(ctx)
	}
	s.contentUsageMu.Lock()
	defer s.contentUsageMu.Unlock()
	if !s.contentUsageAt.IsZero() && time.Since(s.contentUsageAt) < contentUsageCacheTTL {
		return s.contentUsageBytes, nil
	}
	bytes, err := s.measureDatabaseContentUsageBytes(ctx)
	if err != nil {
		return 0, err
	}
	s.contentUsageBytes = bytes
	s.contentUsageAt = time.Now()
	return bytes, nil
}

func (s *Store) measureDatabaseContentUsageBytes(ctx context.Context) (int64, error) {
	var queries []string
	for _, table := range contentUsageTables {
		// Optional module tables may not yet exist. Schema discovery is bounded
		// by this fixed allowlist and does not read user records.
		rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_table_xinfo(?) WHERE hidden=0 ORDER BY cid`, table)
		if err != nil {
			return 0, err
		}
		var sizes []string
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				rows.Close()
				return 0, err
			}
			// Rebuildable columns inside otherwise canonical tables.
			if strings.HasPrefix(column, "filter_") || column == "search_text" || column == "content_refs_json" {
				continue
			}
			identifier := `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
			sizes = append(sizes, `COALESCE(length(CAST(`+identifier+` AS BLOB)),0)`)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		if len(sizes) != 0 {
			queries = append(queries, `SELECT COALESCE(SUM(`+strings.Join(sizes, "+")+`),0) AS bytes FROM `+table)
		}
	}
	if len(queries) == 0 {
		return 0, nil
	}
	var bytes int64
	if err := s.db.QueryRowContext(ctx, `SELECT SUM(bytes) FROM (`+strings.Join(queries, " UNION ALL ")+`)`).Scan(&bytes); err != nil {
		return 0, fmt.Errorf("sum canonical content: %w", err)
	}
	return bytes, nil
}
