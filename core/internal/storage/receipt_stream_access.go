package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// Migration 69 reconciles both receipt previews without changing applied 67.
// Isolated wakeup inserts are new graph leaves. Their current indexed reference
// inputs are checked per page; changes to existing ancestry invalidate the base
// closure. The payload query still validates the full resource_access_epoch.
func reconcileReceiptStreamAccess(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "agent_wakeups")
	if err != nil || !exists {
		return err
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_thread_wakeup ON agent_wakeups(thread_id,wakeup_id)`,
		`DROP VIEW IF EXISTS agent_wakeup_snapshot_positions`,
		`CREATE VIEW agent_wakeup_snapshot_positions AS SELECT wakeup_id,thread_id,trigger_event_id,created_at FROM agent_wakeups`,
		`CREATE TABLE IF NOT EXISTS receipt_visibility_epoch(singleton INTEGER PRIMARY KEY CHECK(singleton=1),version INTEGER NOT NULL)`,
		`INSERT INTO receipt_visibility_epoch VALUES(1,0) ON CONFLICT DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS receipt_access_epoch(singleton INTEGER PRIMARY KEY CHECK(singleton=1),version INTEGER NOT NULL)`,
		`INSERT INTO receipt_access_epoch VALUES(1,0) ON CONFLICT DO NOTHING`,
		`CREATE TRIGGER IF NOT EXISTS receipt_access_replay AFTER UPDATE ON receipt_access_epoch BEGIN UPDATE receipt_visibility_epoch SET version=version+1 WHERE singleton=1; END`,
	}
	if events, err := sqliteTableExists(ctx, tx, "events"); err != nil {
		return err
	} else if events {
		statements = append(statements,
			`CREATE VIEW IF NOT EXISTS receipt_trigger_positions AS SELECT id,trashed_at FROM events`,
			`CREATE TRIGGER IF NOT EXISTS receipt_visibility_event_trash AFTER UPDATE ON events WHEN OLD.trashed_at IS NOT NEW.trashed_at BEGIN UPDATE receipt_visibility_epoch SET version=version+1 WHERE singleton=1; END`)
	}
	for _, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if indexed, err := sqliteTableExists(ctx, tx, "resource_access_exact_edges"); err != nil {
		return err
	} else if !indexed {
		return nil
	}
	// Event identity depends only on ID and handle. Lifecycle changes must not
	// remove/reinsert identical identities and invalidate unrelated readers.
	var identityUpdate string
	if err = tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type='trigger' AND name='mention_identity_events_update'`).Scan(&identityUpdate); err == nil {
		if _, err = tx.ExecContext(ctx, `DROP TRIGGER mention_identity_events_update`); err != nil {
			return err
		}
		identityUpdate = strings.Replace(identityUpdate, "AFTER UPDATE ON events", "AFTER UPDATE OF id,handle ON events", 1)
		if _, err = tx.ExecContext(ctx, identityUpdate); err != nil {
			return err
		}
	} else if err != sql.ErrNoRows {
		return err
	}
	// The complete canonical input set covers parent links, roots, aliases,
	// materialized prose, external keys and tombstones, including direct imports.
	tables := map[string]bool{"ref_edges": true, "resource_handle_aliases": true, "resource_access_tombstones": true, "resource_access_edges": true, "resource_access_identities": true, "resource_access_external_edges": true}
	for _, source := range resourceaccess.OwnershipSources {
		tables[source.Table] = true
	}
	for table := range tables {
		if table == "agent_wakeups" {
			continue
		}
		present, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			when := ""
			if table == "events" && op == "UPDATE" {
				// Lifecycle is checked by the payload query; only ownership
				// inputs invalidate the base denial closure.
				when = ` WHEN OLD.id IS NOT NEW.id OR OLD.handle IS NOT NEW.handle OR OLD.thread_id IS NOT NEW.thread_id OR OLD.refs_json IS NOT NEW.refs_json OR OLD.payload_json IS NOT NEW.payload_json OR OLD.trash_reason IS NOT NEW.trash_reason`
			}
			if table == "resource_access_edges" {
				row := "NEW"
				if op == "DELETE" {
					row = "OLD"
				}
				when = " WHEN " + row + ".source_kind<>'wakeup' OR EXISTS (SELECT 1 FROM resource_access_exact_edges WHERE target_key IN (anx_resource_atom_key(CAST(" + row + ".source_id AS BLOB)),anx_resource_atom_key(CAST(('wakeup:'||" + row + ".source_id) AS BLOB)))) OR EXISTS (SELECT 1 FROM agent_wakeups w WHERE anx_resource_atom_key(CAST(w.wakeup_id AS BLOB))=anx_resource_atom_key(CAST(" + row + ".target_ref AS BLOB)) OR anx_resource_atom_key(CAST(('wakeup:'||w.wakeup_id) AS BLOB))=anx_resource_atom_key(CAST(" + row + ".target_ref AS BLOB)))"
				when += " OR EXISTS (SELECT 1 FROM ref_edges WHERE target_type='wakeup' AND target_id=" + row + ".source_id COLLATE NOCASE)"
				when += " OR EXISTS (SELECT 1 FROM resource_access_identities i JOIN resource_access_mentions m ON m.identity_id=i.identity_id WHERE i.kind='wakeup' AND i.resource_id=" + row + ".source_id)"
				if op == "UPDATE" {
					when = ""
				}
			}
			if table == "resource_access_identities" {
				row := "NEW"
				if op == "DELETE" {
					row = "OLD"
				}
				when = " WHEN " + row + ".kind<>'wakeup' OR EXISTS (SELECT 1 FROM resource_access_exact_edges WHERE target_key IN (anx_resource_atom_key(CAST(" + row + ".ref AS BLOB)),anx_resource_atom_key(CAST(('wakeup:'||" + row + ".ref) AS BLOB)))) OR EXISTS (SELECT 1 FROM resource_access_mention_buckets b JOIN resource_access_edges e ON e.rowid=b.edge_id WHERE b.bucket=" + row + ".bucket AND " + resourceaccess.TextReferenceMatchSQL("e.target_ref", "('wakeup:'||"+row+".ref)") + ")"
				when += " OR EXISTS (SELECT 1 FROM ref_edges WHERE target_type='wakeup' AND target_id=" + row + ".ref COLLATE NOCASE)"
				if op == "UPDATE" {
					when = ""
				} // Direct identity edits always invalidate.
			}
			q := fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS receipt_access_%s_%s AFTER %s ON %s%s BEGIN UPDATE receipt_access_epoch SET version=version+1 WHERE singleton=1; END", table, strings.ToLower(op), op, table, when)
			if _, err = tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	// Source-kind lookups must seek the selected receipt keys, not the ledger.
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS idx_access_exact_source ON resource_access_exact_edges(source_kind,source_id,target_key)`,
		`CREATE INDEX IF NOT EXISTS idx_access_mentions_source ON resource_access_mentions(source_kind,source_id,identity_id)`,
		`CREATE TRIGGER IF NOT EXISTS receipt_access_cross_wakeup_mention AFTER INSERT ON resource_access_mentions WHEN NEW.source_kind='wakeup' AND EXISTS(SELECT 1 FROM resource_access_identities WHERE identity_id=NEW.identity_id AND kind='wakeup') BEGIN UPDATE receipt_access_epoch SET version=version+1 WHERE singleton=1; END`,
		`CREATE INDEX IF NOT EXISTS idx_wakeup_atom ON agent_wakeups(anx_resource_atom_key(CAST(wakeup_id AS BLOB)))`,
		`CREATE INDEX IF NOT EXISTS idx_wakeup_typed_atom ON agent_wakeups(anx_resource_atom_key(CAST(('wakeup:'||wakeup_id) AS BLOB)))`,
		`CREATE TRIGGER IF NOT EXISTS receipt_access_wakeup_delete AFTER DELETE ON agent_wakeups BEGIN UPDATE receipt_access_epoch SET version=version+1 WHERE singleton=1; END`,
		`CREATE TRIGGER IF NOT EXISTS receipt_access_wakeup_update AFTER UPDATE ON agent_wakeups
		 WHEN OLD.wakeup_id IS NOT NEW.wakeup_id OR OLD.thread_id IS NOT NEW.thread_id OR OLD.trigger_event_id IS NOT NEW.trigger_event_id
		 OR OLD.refs_json IS NOT NEW.refs_json OR OLD.trigger_text IS NOT NEW.trigger_text OR OLD.thread_title IS NOT NEW.thread_title OR OLD.failure_reason IS NOT NEW.failure_reason
		 BEGIN UPDATE receipt_access_epoch SET version=version+1 WHERE singleton=1; END`,
		// Cross-receipt references make an insert more than an isolated leaf.
		// Preserve full closure invalidation in both directions, including a
		// previously unresolved bare ID, rather than promoting stale authority.
		`CREATE TRIGGER IF NOT EXISTS receipt_access_wakeup_insert AFTER INSERT ON agent_wakeups WHEN
		 EXISTS (SELECT 1 FROM resource_access_exact_edges WHERE target_key IN (anx_resource_atom_key(CAST(NEW.wakeup_id AS BLOB)),anx_resource_atom_key(CAST(('wakeup:'||NEW.wakeup_id) AS BLOB))))
		 OR EXISTS (SELECT 1 FROM ref_edges WHERE target_type='wakeup' AND target_id=NEW.wakeup_id COLLATE NOCASE)
		 OR EXISTS (SELECT 1 FROM json_each(anx_resource_json_refs(CAST(json_array(json(NEW.refs_json),NEW.trigger_text,NEW.thread_title,NEW.failure_reason) AS BLOB))) j
		 JOIN agent_wakeups w ON anx_resource_atom_key(CAST(w.wakeup_id AS BLOB))=anx_resource_atom_key(CAST(j.value AS BLOB)) OR anx_resource_atom_key(CAST(('wakeup:'||w.wakeup_id) AS BLOB))=anx_resource_atom_key(CAST(j.value AS BLOB)))
		 BEGIN UPDATE receipt_access_epoch SET version=version+1 WHERE singleton=1; END`,
	} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
