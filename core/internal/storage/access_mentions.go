package storage

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Materialize prose inheritance once per write. Both sides have triggers so a
// reference written before its target (or before an alias/rename) stays private.
func installResourceAccessMentions(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS resource_access_epoch(singleton INTEGER PRIMARY KEY CHECK(singleton=1),version INTEGER NOT NULL)`,
		`INSERT INTO resource_access_epoch VALUES(1,0) ON CONFLICT DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS resource_access_identities(identity_id INTEGER PRIMARY KEY,origin TEXT NOT NULL,origin_id TEXT NOT NULL,kind TEXT NOT NULL,resource_id TEXT NOT NULL,ref TEXT NOT NULL,bucket TEXT NOT NULL,UNIQUE(origin,origin_id,kind,resource_id,ref))`,
		`CREATE INDEX IF NOT EXISTS idx_access_identities_resource ON resource_access_identities(kind,resource_id,identity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_access_identities_bucket ON resource_access_identities(bucket,identity_id)`,
		`CREATE TABLE IF NOT EXISTS resource_access_mention_buckets(edge_id INTEGER NOT NULL,bucket TEXT NOT NULL,PRIMARY KEY(edge_id,bucket))`,
		`CREATE INDEX IF NOT EXISTS idx_access_mention_buckets_target ON resource_access_mention_buckets(bucket,edge_id)`,
		`CREATE TABLE IF NOT EXISTS resource_access_exact_edges(edge_id INTEGER PRIMARY KEY,source_kind TEXT NOT NULL,source_id TEXT NOT NULL,target_key TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_access_exact_edges_target ON resource_access_exact_edges(target_key,source_kind,source_id)`,
		`CREATE TABLE IF NOT EXISTS resource_access_mentions(edge_id INTEGER NOT NULL,identity_id INTEGER NOT NULL,source_kind TEXT NOT NULL,source_id TEXT NOT NULL,PRIMARY KEY(edge_id,identity_id))`,
		`CREATE INDEX IF NOT EXISTS idx_access_mentions_target ON resource_access_mentions(identity_id,source_kind,source_id)`,
		`CREATE TRIGGER IF NOT EXISTS access_identity_insert AFTER INSERT ON resource_access_identities BEGIN
   INSERT INTO resource_access_mentions SELECT b.edge_id,NEW.identity_id,e.source_kind,e.source_id FROM resource_access_mention_buckets b JOIN resource_access_edges e ON e.rowid=b.edge_id
   WHERE b.bucket=NEW.bucket AND ` + resourceaccess.TextReferenceMatchSQL("e.target_ref", "(NEW.kind||':'||NEW.ref)") + ` ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS access_identity_delete AFTER DELETE ON resource_access_identities BEGIN DELETE FROM resource_access_mentions WHERE identity_id=OLD.identity_id; END`,
	}
	statements = append(statements, `DELETE FROM resource_access_exact_edges`, `DELETE FROM resource_access_mentions`, `DELETE FROM resource_access_mention_buckets`, `DELETE FROM resource_access_identities`)

	buckets := func(row, from string) string {
		return `INSERT INTO resource_access_mention_buckets SELECT ` + row + `rowid,j.value FROM ` + from + `json_each(anx_resource_mention_buckets(CAST(` + row + `target_ref AS BLOB))) j WHERE true ON CONFLICT DO NOTHING;`
	}
	resolve := `INSERT INTO resource_access_mentions SELECT NEW.rowid,i.identity_id,NEW.source_kind,NEW.source_id FROM resource_access_mention_buckets b JOIN resource_access_identities i ON i.bucket=b.bucket WHERE b.edge_id=NEW.rowid AND ` + resourceaccess.TextReferenceMatchSQL("NEW.target_ref", "(i.kind||':'||i.ref)") + ` ON CONFLICT DO NOTHING;`
	clear := `DELETE FROM resource_access_exact_edges WHERE edge_id=OLD.rowid; DELETE FROM resource_access_mentions WHERE edge_id=OLD.rowid; DELETE FROM resource_access_mention_buckets WHERE edge_id=OLD.rowid;`
	statements = append(statements,
		`CREATE TRIGGER IF NOT EXISTS access_mention_edge_insert AFTER INSERT ON resource_access_edges BEGIN `+buckets("NEW.", "")+`INSERT INTO resource_access_exact_edges VALUES(NEW.rowid,NEW.source_kind,NEW.source_id,anx_resource_atom_key(CAST(NEW.target_ref AS BLOB))) ON CONFLICT DO NOTHING;`+resolve+` END`,
		`CREATE TRIGGER IF NOT EXISTS access_mention_edge_update AFTER UPDATE ON resource_access_edges BEGIN `+clear+buckets("NEW.", "")+`INSERT INTO resource_access_exact_edges VALUES(NEW.rowid,NEW.source_kind,NEW.source_id,anx_resource_atom_key(CAST(NEW.target_ref AS BLOB))) ON CONFLICT DO NOTHING;`+resolve+` END`,
		`CREATE TRIGGER IF NOT EXISTS access_mention_edge_delete AFTER DELETE ON resource_access_edges BEGIN `+clear+` END`,
		buckets("e.", "resource_access_edges e, "),
		`INSERT INTO resource_access_exact_edges SELECT rowid,source_kind,source_id,anx_resource_atom_key(CAST(target_ref AS BLOB)) FROM resource_access_edges WHERE true ON CONFLICT DO NOTHING`,
	)
	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("install mention index: %w", err)
		}
	}

	// Series provenance is append-only across compaction. Feed the same atomic
	// index so reading rollups never scans retained contributor prose either.
	if exists, err := sqliteTableExists(ctx, tx, "resource_access_series_refs"); err != nil {
		return err
	} else if exists {
		insert := func(prefix, from string) string {
			return `INSERT INTO resource_access_edges SELECT 'series',json_array(` + prefix + `series,` + prefix + `labels),` + prefix + `target_ref FROM ` + from + `(SELECT 1) WHERE true ON CONFLICT DO NOTHING;`
		}
		for _, q := range []string{
			insert("r.", "resource_access_series_refs r, "),
			`CREATE TRIGGER IF NOT EXISTS mention_series_refs_insert AFTER INSERT ON resource_access_series_refs BEGIN ` + insert("NEW.", "") + ` END`,
			`CREATE TRIGGER IF NOT EXISTS mention_series_refs_delete AFTER DELETE ON resource_access_series_refs BEGIN DELETE FROM resource_access_edges WHERE source_kind='series' AND source_id=json_array(OLD.series,OLD.labels) AND target_ref=OLD.target_ref; END`,
			`CREATE TRIGGER IF NOT EXISTS mention_series_refs_update AFTER UPDATE ON resource_access_series_refs BEGIN DELETE FROM resource_access_edges WHERE source_kind='series' AND source_id=json_array(OLD.series,OLD.labels) AND target_ref=OLD.target_ref; ` + insert("NEW.", "") + ` END`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}

	type identitySource struct{ table, id, kind, resourceID, refs string }
	var sources []identitySource
	// Numeric evidence projection IDs are internal, never navigable identities.
	// Their tables still invalidate request snapshots below.
	for _, s := range resourceaccess.OwnershipSources {
		if s.Kind == "work_evidence_record" || s.Kind == "work_evidence_alias" {
			continue
		}
		refs := "r." + s.ID
		switch s.Kind {
		case "thread", "board", "card", "topic", "document", "event", "artifact", "run":
			refs += ",r.handle"
		}
		// Each expression contributes independently, not a concatenated identity.
		sources = append(sources, identitySource{s.Table, "r." + s.ID, "'" + s.Kind + "'", "r." + s.ID, refs})
	}
	sources = append(sources,
		identitySource{"resource_handle_aliases", "json_array(r.resource_type,r.alias_handle)", "r.resource_type", "r.resource_id", "r.alias_handle"},
		identitySource{"resource_access_tombstones", "json_array(r.kind,r.id,r.ref,r.owner)", "r.kind", "r.id", "r.ref"},
	)
	for _, s := range sources {
		exists, err := sqliteTableExists(ctx, tx, s.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		var refs []string
		for _, ref := range strings.Split(s.refs, ",") {
			// Legacy migration fixtures can have a partial schema.
			col := strings.TrimPrefix(ref, "r.")
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, s.table, col).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				refs = append(refs, ref)
			}
		}
		insert := func(prefix, from string) string {
			var parts []string
			replace := func(v string) string { return strings.ReplaceAll(v, "r.", prefix) }
			for _, ref := range refs {
				value := replace(ref)
				parts = append(parts, `SELECT '`+s.table+`',`+replace(s.id)+`,`+replace(s.kind)+`,`+replace(s.resourceID)+`,`+value+`,`+replace(s.kind)+`||':'||anx_resource_mention_bucket(CAST(`+value+` AS BLOB)) FROM `+from+`(SELECT 1) WHERE COALESCE(`+value+`,'')<>''`)
			}
			if len(parts) == 0 {
				return ""
			}
			return `INSERT INTO resource_access_identities(origin,origin_id,kind,resource_id,ref,bucket) ` + strings.Join(parts, " UNION ") + ` ON CONFLICT DO NOTHING;`
		}
		clear := `DELETE FROM resource_access_identities WHERE origin='` + s.table + `' AND origin_id=` + strings.ReplaceAll(s.id, "r.", "OLD.") + `;`
		for _, q := range []string{
			insert("r.", s.table+" r, "),
			`CREATE TRIGGER IF NOT EXISTS mention_identity_` + s.table + `_insert AFTER INSERT ON ` + s.table + ` BEGIN ` + insert("NEW.", "") + ` END`,
			`CREATE TRIGGER IF NOT EXISTS mention_identity_` + s.table + `_update AFTER UPDATE ON ` + s.table + ` BEGIN ` + clear + insert("NEW.", "") + ` END`,
			`CREATE TRIGGER IF NOT EXISTS mention_identity_` + s.table + `_delete AFTER DELETE ON ` + s.table + ` BEGIN ` + clear + ` END`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("index %s identities: %w", s.table, err)
			}
		}
	}
	// Large ref-edge metadata must not be fetched just to traverse endpoints.
	// Likewise board-child probes must never scan all cards.
	for table, q := range map[string]string{
		"ref_edges": `CREATE INDEX IF NOT EXISTS idx_ref_edges_access_cover ON ref_edges(target_type,target_id COLLATE NOCASE,edge_type,source_type,source_id)`,
		"threads":   `CREATE INDEX IF NOT EXISTS idx_access_private_threads ON threads(json_extract(body_json,'$.pm_actor_id'),id) WHERE COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>''`,
		"artifacts": `CREATE INDEX IF NOT EXISTS idx_access_unknown_artifacts ON artifacts(id) WHERE content_refs_json IS NULL`,
		"cards":     `CREATE INDEX IF NOT EXISTS idx_cards_access_board ON cards(board_id,id)`,
	} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if exists {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	if err := installResourceAccessRevisionHandles(ctx, tx); err != nil {
		return err
	}
	// Every write that can change roots, structural parents or reference ancestry
	// invalidates cached request snapshots. Broad invalidation is conservative.
	tables := map[string]bool{"ref_edges": true, "resource_handle_aliases": true, "resource_access_tombstones": true, "derived_inbox_items": true, "resource_access_edges": true, "resource_access_identities": true, "resource_access_external_edges": true}
	for _, source := range resourceaccess.OwnershipSources {
		tables[source.Table] = true
	}
	for table := range tables {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			q := `CREATE TRIGGER IF NOT EXISTS access_epoch_` + table + `_` + strings.ToLower(op) + ` AFTER ` + op + ` ON ` + table + ` BEGIN UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1; END`
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}

	return nil
}

func installResourceAccessRevisionHandles(ctx context.Context, tx *sql.Tx) error {
	// Virtual revision handles depend on both revision rows and parent handles.
	for _, kind := range []string{"document", "card"} {
		parent, revisions := kind+"s", kind+"_revisions"
		exists, err := sqliteTableExists(ctx, tx, revisions)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		insert := func(where string) string {
			return `INSERT INTO resource_access_identities(origin,origin_id,kind,resource_id,ref,bucket)
   SELECT '` + revisions + `_handle',r.revision_id,'` + kind + `_revision',r.revision_id,COALESCE(NULLIF(p.handle,''),p.id)||'-r'||r.revision_number,
   '` + kind + `_revision:'||anx_resource_mention_bucket(CAST(COALESCE(NULLIF(p.handle,''),p.id)||'-r'||r.revision_number AS BLOB))
   FROM ` + revisions + ` r JOIN ` + parent + ` p ON p.id=r.` + kind + `_id WHERE ` + where + ` ON CONFLICT DO NOTHING;`
		}
		clear := `DELETE FROM resource_access_identities WHERE origin='` + revisions + `_handle' AND origin_id=OLD.revision_id;`
		clearParent := `DELETE FROM resource_access_identities WHERE origin='` + revisions + `_handle' AND origin_id IN (SELECT revision_id FROM ` + revisions + ` WHERE ` + kind + `_id=OLD.id);`
		for _, q := range []string{
			insert("true"),
			`CREATE TRIGGER IF NOT EXISTS mention_` + revisions + `_handle_insert AFTER INSERT ON ` + revisions + ` BEGIN ` + insert("r.revision_id=NEW.revision_id") + ` END`,
			`CREATE TRIGGER IF NOT EXISTS mention_` + revisions + `_handle_update AFTER UPDATE ON ` + revisions + ` BEGIN ` + clear + insert("r.revision_id=NEW.revision_id") + ` END`,
			`CREATE TRIGGER IF NOT EXISTS mention_` + revisions + `_handle_delete AFTER DELETE ON ` + revisions + ` BEGIN ` + clear + ` END`,
			`CREATE TRIGGER IF NOT EXISTS mention_` + parent + `_revision_handles_insert AFTER INSERT ON ` + parent + ` BEGIN ` + insert("r."+kind+"_id=NEW.id") + ` END`,
			`CREATE TRIGGER IF NOT EXISTS mention_` + parent + `_revision_handles_update AFTER UPDATE OF handle,id ON ` + parent + ` BEGIN ` + clearParent + insert("r."+kind+"_id=NEW.id") + ` END`,
			`CREATE TRIGGER IF NOT EXISTS mention_` + parent + `_revision_handles_delete AFTER DELETE ON ` + parent + ` BEGIN ` + clearParent + ` END`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("index revision handles: %w", err)
			}
		}
	}
	return nil
}
