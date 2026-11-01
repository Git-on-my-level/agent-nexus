// Resource-access migrations (55-60): installing and batching
// resource_access edges, and repairing legacy or NUL-truncated previews.

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

func installResourceAccess(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS resource_access_tombstones(kind TEXT NOT NULL,id TEXT NOT NULL,ref TEXT NOT NULL,owner TEXT NOT NULL,PRIMARY KEY(kind,id,ref,owner));`,
		`CREATE TABLE IF NOT EXISTS resource_access_edges(source_kind TEXT NOT NULL,source_id TEXT NOT NULL,target_ref TEXT NOT NULL COLLATE NOCASE,PRIMARY KEY(source_kind,source_id,target_ref));`,
		`CREATE INDEX IF NOT EXISTS idx_resource_access_edges_target ON resource_access_edges(target_ref,source_kind,source_id);`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	if err := installResourceAccessEdges(ctx, tx); err != nil {
		return err
	}
	// Privacy-54 previews used an unnormalized expression under this name.
	// Rebuild it so reconciliation cannot retain an index the graph cannot use.
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_work_access_project`); err != nil {
		return err
	}
	// Some legacy migration tests intentionally have only a partial schema.
	for table, statement := range map[string]string{
		"ref_edges":           `CREATE INDEX IF NOT EXISTS idx_ref_edges_access_target ON ref_edges(target_type,target_id COLLATE NOCASE,edge_type)`,
		"cards":               `CREATE INDEX IF NOT EXISTS idx_cards_access_thread ON cards(thread_id)`,
		"derived_inbox_items": `CREATE INDEX IF NOT EXISTS idx_inbox_access_card ON derived_inbox_items(source_card_id)`,
		"work_metadata":       `CREATE INDEX IF NOT EXISTS idx_work_access_project ON work_metadata(` + resourceaccess.ReferenceSQL("json_extract(metadata_json,'$.project_ref')") + ` COLLATE NOCASE)`,
		"agent_wakeups":       `CREATE INDEX IF NOT EXISTS idx_wakeups_access_trigger_event ON agent_wakeups(trigger_event_id)`,
		"card_revisions":      `CREATE INDEX IF NOT EXISTS idx_card_revisions_access_artifact ON card_revisions(artifact_id)`,
		"document_revisions":  `CREATE INDEX IF NOT EXISTS idx_document_revisions_access_artifact ON document_revisions(artifact_id)`,
	} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if exists, err := sqliteTableExists(ctx, tx, "derived_inbox_items"); err != nil {
		return err
	} else if exists {
		_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_inbox_access_event ON derived_inbox_items(source_event_id)`)
		return err
	}
	return nil
}

func installCompleteResourceAccess(ctx context.Context, tx *sql.Tx) error {
	// Aggregates can outlive raw points. Retain reference provenance per label
	// stream so compaction cannot turn a formerly private contributor public.
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS resource_access_series_refs(series TEXT NOT NULL,labels TEXT NOT NULL,target_ref TEXT NOT NULL COLLATE NOCASE,PRIMARY KEY(series,labels,target_ref))`); err != nil {
		return err
	}
	for table, state := range map[string]string{"series_points": "state", "series_daily": "last_state", "series_live_daily": "last_state"} {
		if exists, err := sqliteTableExists(ctx, tx, table); err != nil {
			return err
		} else if !exists {
			continue
		}
		insert := func(prefix, from string) string {
			return `INSERT INTO resource_access_series_refs SELECT ` + prefix + `series,` + prefix + `labels,j.value FROM ` + from + `json_each(anx_resource_json_refs(CAST(json_array(json(` + prefix + `labels),` + prefix + state + `) AS BLOB))) j WHERE j.value<>'' ON CONFLICT DO NOTHING;`
		}
		for _, statement := range []string{insert("r.", table+" r, "), `DROP TRIGGER IF EXISTS access_` + table + `_insert`, `DROP TRIGGER IF EXISTS access_` + table + `_update`, `CREATE TRIGGER IF NOT EXISTS access_` + table + `_insert AFTER INSERT ON ` + table + ` BEGIN ` + insert("NEW.", "") + ` END`, `CREATE TRIGGER IF NOT EXISTS access_` + table + `_update AFTER UPDATE ON ` + table + ` BEGIN ` + insert("NEW.", "") + ` END`} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	if exists, err := sqliteTableExists(ctx, tx, "artifacts"); err != nil {
		return err
	} else if exists {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('artifacts') WHERE name='content_refs_json'`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE artifacts ADD COLUMN content_refs_json TEXT`); err != nil {
				return err
			}
		}
	}
	// Rebuild rather than union with the old index: work metadata previously
	// shared the card source kind, which would collide with canonical card fields.
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_access_edges`); err != nil {
		return err
	}
	return installResourceAccess(ctx, tx)
}

// Index canonical JSON references in the same statement as their records. This
// covers every producer (including imports), and backfills existing workspaces.
// These ownership edges are separate from navigational ref_edges: replacing a
// record's top-level refs must never discard payload-derived authorization.
func installResourceAccessEdges(ctx context.Context, tx *sql.Tx) error {
	return installResourceAccessSourceEdges(ctx, tx, resourceaccess.OwnershipSources)
}

func installResourceAccessSourceEdges(ctx context.Context, tx *sql.Tx, sources []resourceaccess.OwnershipSource) error {
	return installResourceAccessSourceEdgesBatched(ctx, tx, sources, 0)
}

// Existing main migrations retain their historical backfill. Feature upgrades
// register only changed sources and hydrate a bounded keyset page at a time.
func installResourceAccessSourceEdgesBatched(ctx context.Context, tx *sql.Tx, sources []resourceaccess.OwnershipSource, batchSize int) error {
	for _, source := range sources {
		exists, err := sqliteTableExists(ctx, tx, source.Table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		// Legacy partial schemas and the earlier migrations may lack later fields.
		var columns []string
		for _, column := range source.Columns {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, source.Table, column).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				columns = append(columns, column)
			}
		}
		if len(columns) == 0 {
			continue
		}
		insert := func(prefix, from string) string {
			var parts []string
			for _, column := range columns {
				value := prefix + column
				// Imported inbox parents can use handles/aliases. Preserve the
				// typed spelling checked by the former per-item HTTP guard.
				if source.Table == "derived_inbox_items" {
					kind := map[string]string{"thread_id": "thread", "source_card_id": "card", "source_event_id": "event"}[column]
					if kind != "" {
						// Keep raw atoms too: legacy scalar IDs can themselves be
						// encoded JSON or contain references to another kind.
						parts = append(parts, `SELECT '`+source.Kind+`',`+prefix+source.ID+`,j.value FROM `+from+`json_each(`+resourceaccess.ReferenceSQLAtoms(value, false)+`) j WHERE j.value<>''`)
						value = "('" + kind + ":'||" + value + ")"
					}
				}
				parts = append(parts, `SELECT '`+source.Kind+`',`+prefix+source.ID+`,j.value FROM `+from+`json_each(`+resourceaccess.ReferenceSQLAtoms(value, strings.HasSuffix(column, "_json"))+`) j WHERE j.value<>''`)
			}
			// An outer UPSERT can override a trigger's legacy OR IGNORE policy.
			// An explicit conflict target remains safe for duplicate JSON atoms.
			return `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) ` + strings.Join(parts, ` UNION `) + ` ON CONFLICT(source_kind,source_id,target_ref) DO NOTHING;`
		}
		clear := `DELETE FROM resource_access_edges WHERE source_kind='` + source.Kind + `' AND source_id=OLD.` + source.ID + `;`
		backfill := []string{insert("r.", source.Table+" r, ")}
		if batchSize > 0 {
			backfill = nil
			var cursor any
			for {
				query := `SELECT ` + source.ID + ` FROM ` + source.Table
				args := []any{}
				if cursor != nil {
					query += ` WHERE ` + source.ID + `>?`
					args = append(args, cursor)
				}
				query += ` ORDER BY ` + source.ID + ` LIMIT ?`
				args = append(args, batchSize)
				rows, err := tx.QueryContext(ctx, query, args...)
				if err != nil {
					return err
				}
				ids := []any{}
				for rows.Next() {
					var id any
					if err = rows.Scan(&id); err != nil {
						rows.Close()
						return err
					}
					ids = append(ids, id)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return err
				}
				if len(ids) == 0 {
					break
				}
				encoded, err := json.Marshal(ids)
				if err != nil {
					return err
				}
				// Materialize only this requested page; every column matcher shares
				// the same binding and cannot decode unrelated workspace rows.
				q := `WITH access_backfill_batch AS MATERIALIZED (SELECT * FROM ` + source.Table + ` WHERE ` + source.ID + ` IN (SELECT value FROM json_each(?))) ` + insert("r.", "access_backfill_batch r, ")
				if _, err = tx.ExecContext(ctx, q, string(encoded)); err != nil {
					return err
				}
				cursor = ids[len(ids)-1]
			}
		}
		statements := []string{
			`DROP TRIGGER IF EXISTS access_` + source.Table + `_insert`,
			`DROP TRIGGER IF EXISTS access_` + source.Table + `_update`,
			`DROP TRIGGER IF EXISTS access_` + source.Table + `_delete`,
		}
		statements = append(statements, backfill...)
		statements = append(statements,
			`CREATE TRIGGER IF NOT EXISTS access_`+source.Table+`_insert AFTER INSERT ON `+source.Table+` BEGIN `+insert("NEW.", "")+` END`,
			`CREATE TRIGGER IF NOT EXISTS access_`+source.Table+`_update AFTER UPDATE OF `+strings.Join(append([]string{source.ID}, columns...), ",")+` ON `+source.Table+` BEGIN `+clear+insert("NEW.", "")+` END`,
			`CREATE TRIGGER IF NOT EXISTS access_`+source.Table+`_delete AFTER DELETE ON `+source.Table+` BEGIN `+clear+` END`,
		)
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("index %s authorization: %w", source.Table, err)
			}
		}
	}
	return nil
}

// Earlier compaction did not preserve every contributor. Keep that uncertainty
// until explicit data deletion; a later public point cannot certify old totals.
func repairLegacyReferenceAccess(ctx context.Context, tx *sql.Tx) error {
	if err := installResourceAccessEdges(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS resource_access_series_unknown(series TEXT NOT NULL,labels TEXT NOT NULL,PRIMARY KEY(series,labels))`); err != nil {
		return err
	}
	for _, table := range []string{"series_daily", "series_live_daily"} {
		if exists, err := sqliteTableExists(ctx, tx, table); err != nil {
			return err
		} else if exists {
			if _, err = tx.ExecContext(ctx, `INSERT INTO resource_access_series_unknown SELECT series,labels FROM `+table+` WHERE true ON CONFLICT DO NOTHING`); err != nil {
				return err
			}
		}
	}
	// Retained state text may be the only surviving source after compaction.
	// Add the new prose candidates without discarding historical ledger atoms.
	if exists, err := sqliteTableExists(ctx, tx, "resource_access_series_refs"); err != nil {
		return err
	} else if exists {
		if _, err = tx.ExecContext(ctx, `INSERT INTO resource_access_series_refs SELECT r.series,r.labels,j.value FROM resource_access_series_refs r,json_each(anx_resource_refs(CAST(r.target_ref AS BLOB))) j WHERE j.value<>'' ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
	}
	if exists, err := sqliteTableExists(ctx, tx, "artifacts"); err != nil {
		return err
	} else if exists {
		if _, err = tx.ExecContext(ctx, `UPDATE artifacts SET content_refs_json=NULL`); err != nil {
			return err
		}
	}
	return installCompleteResourceAccess(ctx, tx)
}

func repairNULReferenceAccess(ctx context.Context, tx *sql.Tx) error {
	// Replace legacy TEXT-argument triggers before any canonical UPDATE fires.
	if err := installCompleteResourceAccess(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_access_series_refs SELECT r.series,r.labels,j.value FROM resource_access_series_refs r,json_each(anx_resource_refs(CAST(r.target_ref AS BLOB))) j WHERE j.value<>'' ON CONFLICT DO NOTHING`); err != nil {
		return err
	}
	// Rescan blobs with the configured backend; retain canonical bytes and fail
	// closed while a manifest is unavailable. Existing NUL content is not erased.
	if exists, err := sqliteTableExists(ctx, tx, "artifacts"); err != nil {
		return err
	} else if exists {
		_, err = tx.ExecContext(ctx, `UPDATE artifacts SET content_refs_json=NULL`)
		return err
	}
	return nil
}
