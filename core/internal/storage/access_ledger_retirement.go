package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// Migration 150 completes the compact-access contraction (SCA-774). The legacy
// exact-edge ledger and the admission compact-atom mirror are derived copies of
// the compact ledger. Before either is dropped, the compact ledger is compared
// with the legacy ledger and the raw edge table in both EXCEPT directions. Any
// divergence aborts the upgrade; the migration never repairs or guesses.
//
// The compact capability is installed at open today, not by a numbered
// migration. This migration makes that dependency explicit so a fresh workspace
// and an upgraded workspace reach the same schema.
func retireLegacyAccessLedger(ctx context.Context, tx *sql.Tx) error {
	if err := ensureCompactAccessLedger(ctx, tx); err != nil {
		return err
	}
	// Stop the legacy writers before the tables they reference disappear. This
	// also prevents an interrupted compact backfill from replaying admission
	// links through the old compact-atom sync triggers.
	for _, q := range []string{
		`DROP TRIGGER IF EXISTS access_compact_insert`,
		`DROP TRIGGER IF EXISTS access_compact_update`,
		`DROP TRIGGER IF EXISTS access_compact_delete`,
		`DROP TRIGGER IF EXISTS compact_generation_resource_access_exact_edges_insert`,
		`DROP TRIGGER IF EXISTS compact_generation_resource_access_exact_edges_update`,
		`DROP TRIGGER IF EXISTS compact_generation_resource_access_exact_edges_delete`,
		`DROP TRIGGER IF EXISTS admission_compact_sync_insert`,
		`DROP TRIGGER IF EXISTS admission_compact_sync_update`,
		`DROP TRIGGER IF EXISTS admission_compact_sync_delete`,
		`DROP TRIGGER IF EXISTS admission_key_insert`,
		`DROP TRIGGER IF EXISTS admission_key_delete`,
		`DROP TRIGGER IF EXISTS admission_exact_insert`,
		`DROP TRIGGER IF EXISTS admission_exact_update`,
		`DROP TRIGGER IF EXISTS admission_exact_delete`,
		`DROP TRIGGER IF EXISTS admission_compact_insert`,
		`DROP TRIGGER IF EXISTS admission_compact_update`,
		`DROP TRIGGER IF EXISTS admission_compact_delete`,
		`DROP TRIGGER IF EXISTS admission_compact_source_insert`,
		`DROP TRIGGER IF EXISTS admission_compact_source_update`,
		`DROP TRIGGER IF EXISTS admission_compact_source_delete`,
		`DROP TRIGGER IF EXISTS access_mention_edge_insert`,
		`DROP TRIGGER IF EXISTS access_mention_edge_update`,
		`DROP TRIGGER IF EXISTS access_mention_edge_delete`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("retire legacy access ledger: %w", err)
		}
	}
	if err := completeCompactAccessLedger(ctx, tx); err != nil {
		return err
	}
	if err := requireStableAccessEdges(ctx, tx); err != nil {
		return err
	}
	if err := verifyLegacyExactLedgerEquivalent(ctx, tx); err != nil {
		return err
	}
	if err := rebuildCompactAdmissionLinks(ctx, tx); err != nil {
		return err
	}
	for _, q := range []string{
		`DROP TABLE IF EXISTS resource_access_admission_compact_atoms`,
		`DROP TABLE IF EXISTS resource_access_exact_edges`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("retire legacy access ledger: %w", err)
		}
	}
	if err := reinstallMentionEdgeWriters(ctx, tx); err != nil {
		return err
	}
	if err := reinstallAdmissionLedgerTriggers(ctx, tx); err != nil {
		return err
	}
	// Reapply the update-program narrowing that migrations 116 and 117 installed
	// on the retired trigger bodies. The new bodies reference the same inputs.
	if err := restrictAdmissionUpdates(ctx, tx); err != nil {
		return err
	}
	if err := restrictAdmissionUpdatePrograms(ctx, tx); err != nil {
		return err
	}
	if err := reinstallReceiptAccessTriggers(ctx, tx); err != nil {
		return err
	}
	if err := installAdmissionLinkIndexes(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET reader=1,stable=1,force_legacy=0 WHERE singleton=1`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_job SET done=1 WHERE singleton=1`); err != nil {
		return err
	}
	return verifyRetiredAccessLedger(ctx, tx)
}

// ensureCompactAccessLedger installs the compact capability that admission now
// requires. It is idempotent and upgrades control tables written by earlier
// builds that predate force_legacy.
func ensureCompactAccessLedger(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS resource_access_compact_control(singleton INTEGER PRIMARY KEY CHECK(singleton=1), reader INTEGER NOT NULL DEFAULT 0 CHECK(reader IN (0,1,2)), stable INTEGER NOT NULL DEFAULT 0, generation INTEGER NOT NULL DEFAULT 0, raw_cursor INTEGER, raw_done INTEGER NOT NULL DEFAULT 0, force_legacy INTEGER NOT NULL DEFAULT 0 CHECK(force_legacy IN (0,1)))`,
		`INSERT INTO resource_access_compact_control(singleton) VALUES(1) ON CONFLICT DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS resource_access_compact_edges(edge_id INTEGER PRIMARY KEY,target_key BLOB NOT NULL CHECK(typeof(target_key)='blob' AND length(target_key)=32))`,
		`CREATE INDEX IF NOT EXISTS idx_access_compact_target ON resource_access_compact_edges(target_key)`,
		`CREATE TABLE IF NOT EXISTS resource_access_compact_job(singleton INTEGER PRIMARY KEY CHECK(singleton=1), cursor INTEGER, done INTEGER NOT NULL CHECK(done IN (0,1)))`,
		`INSERT INTO resource_access_compact_job VALUES(1,NULL,0) ON CONFLICT DO NOTHING`,
		// Verification-only relation; no production read selects it.
		`CREATE VIEW IF NOT EXISTS resource_access_compact_reconstructed AS
 SELECT c.edge_id,e.source_kind,e.source_id,lower(hex(c.target_key)) AS target_key
 FROM resource_access_compact_edges c JOIN resource_access_edges e ON e.rowid=c.edge_id`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("install compact access ledger: %w", err)
		}
	}
	var hasOverride bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('resource_access_compact_control') WHERE name='force_legacy')`).Scan(&hasOverride); err != nil {
		return err
	}
	if !hasOverride {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_access_compact_control ADD COLUMN force_legacy INTEGER NOT NULL DEFAULT 0 CHECK(force_legacy IN (0,1))`); err != nil {
			return err
		}
	}
	return nil
}

// completeCompactAccessLedger finishes an interrupted compact backfill from the
// legacy ledger before verification. A completed ledger is never overwritten:
// the job's done flag distinguishes a certified snapshot from partial progress.
func completeCompactAccessLedger(ctx context.Context, tx *sql.Tx) error {
	exact, err := sqliteTableExists(ctx, tx, "resource_access_exact_edges")
	if err != nil || !exact {
		return err
	}
	var done bool
	if err := tx.QueryRowContext(ctx, `SELECT done FROM resource_access_compact_job WHERE singleton=1`).Scan(&done); err != nil {
		return err
	}
	if done {
		return nil
	}
	// An uncertified ledger may contain partial or stale rows from an earlier
	// build. The raw edge table is canonical until certification, so rebuild the
	// derived exact and compact ledgers exactly rather than merging into them.
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_access_exact_edges`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_access_exact_edges SELECT rowid,source_kind,source_id,anx_resource_atom_key(CAST(target_ref AS BLOB)) FROM resource_access_edges WHERE true ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_key=excluded.target_key`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_access_compact_edges`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_access_compact_edges SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE true ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key`); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE resource_access_compact_job SET done=1 WHERE singleton=1`)
	return err
}

// requireStableAccessEdges completes the raw-edge identity cutover when the
// online worker has not done so. The staged table is populated from the raw
// table, verified in both directions, then renamed into place. The retired
// unstable table is drained by the existing maintenance worker.
func requireStableAccessEdges(ctx context.Context, tx *sql.Tx) error {
	var stable bool
	if err := tx.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
		return err
	}
	if stable {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS resource_access_stable_edges(edge_id INTEGER PRIMARY KEY,source_kind TEXT NOT NULL,source_id TEXT NOT NULL,target_ref TEXT NOT NULL COLLATE NOCASE,UNIQUE(source_kind,source_id,target_ref))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_access_stable_edges SELECT rowid,source_kind,source_id,target_ref FROM resource_access_edges WHERE true ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_ref=excluded.target_ref`); err != nil {
		return err
	}
	old := `SELECT rowid,source_kind,source_id,CAST(target_ref AS BLOB) FROM resource_access_edges`
	staged := `SELECT edge_id,source_kind,source_id,CAST(target_ref AS BLOB) FROM resource_access_stable_edges`
	for _, q := range []string{`SELECT EXISTS(` + old + ` EXCEPT ` + staged + `)`, `SELECT EXISTS(` + staged + ` EXCEPT ` + old + `)`} {
		var mismatch bool
		if err := tx.QueryRowContext(ctx, q).Scan(&mismatch); err != nil {
			return err
		}
		if mismatch {
			return errors.New("staged raw edge identities differ from the live table")
		}
	}
	if err := stabilizeAccessEdgesTx(ctx, tx); err != nil {
		return err
	}
	// A fresh workspace's retired table is empty; drop it now so idle
	// maintenance never needs a writer just to discover there is nothing to do.
	var empty bool
	if err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM resource_access_unstable_edges)`).Scan(&empty); err != nil {
		return err
	}
	if empty {
		if _, err := tx.ExecContext(ctx, `DROP TABLE resource_access_unstable_edges`); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET stable=1 WHERE singleton=1`)
	return err
}

// verifyLegacyExactLedgerEquivalent reuses the compact migration's both-way
// EXCEPT checks. It also compares the legacy ledger with independently hashed
// raw edges, so an equally corrupt old/new pair is not accepted.
func verifyLegacyExactLedgerEquivalent(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "resource_access_exact_edges")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	legacy := `SELECT edge_id,source_kind,source_id,target_key FROM resource_access_exact_edges`
	reconstructed := `SELECT c.edge_id,e.source_kind,e.source_id,lower(hex(c.target_key)) FROM resource_access_compact_edges c JOIN resource_access_edges e ON e.rowid=c.edge_id`
	raw := `SELECT rowid,source_kind,source_id,anx_resource_atom_key(CAST(target_ref AS BLOB)) FROM resource_access_edges`
	for _, check := range []struct{ name, query string }{
		{"exact_to_compact", `SELECT EXISTS(` + legacy + ` EXCEPT ` + reconstructed + `)`},
		{"compact_to_exact", `SELECT EXISTS(` + reconstructed + ` EXCEPT ` + legacy + `)`},
		{"exact_to_raw", `SELECT EXISTS(` + legacy + ` EXCEPT ` + raw + `)`},
		{"raw_to_exact", `SELECT EXISTS(` + raw + ` EXCEPT ` + legacy + `)`},
		{"orphan_compact_edge", `SELECT EXISTS(SELECT 1 FROM resource_access_compact_edges c LEFT JOIN resource_access_edges e ON e.rowid=c.edge_id WHERE e.rowid IS NULL)`},
	} {
		var mismatch bool
		if err := tx.QueryRowContext(ctx, check.query).Scan(&mismatch); err != nil {
			return err
		}
		if mismatch {
			return fmt.Errorf("legacy access ledger is not equivalent to the compact ledger (%s); refusing to drop it", check.name)
		}
	}
	return nil
}

// rebuildCompactAdmissionLinks replaces the compact-origin links with rows
// derived from the verified compact ledger. An interrupted compact backfill can
// leave ledger 2 incomplete even though ledger 1 is complete; the graph, reach
// and owner triggers remain installed and update those ledgers as links change.
func rebuildCompactAdmissionLinks(ctx context.Context, tx *sql.Tx) error {
	publication := `('work_metadata','work_observation','work_evidence_record','work_evidence_alias')`
	for _, q := range []string{
		`DELETE FROM resource_access_admission_links WHERE origin='compact'`,
		`INSERT OR IGNORE INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger)
SELECT 'compact',e.rowid,k.key_id,k.kind,k.id,e.source_kind,e.source_id,2
FROM resource_access_compact_edges c
JOIN resource_access_edges e ON e.rowid=c.edge_id
JOIN resource_access_admission_keys k ON k.atom=c.target_key
WHERE k.published=0 OR e.source_kind NOT IN ` + publication,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("rebuild compact admission links: %w", err)
		}
	}
	return nil
}

// reinstallMentionEdgeWriters makes the compact ledger the only edge digest
// written by canonical edge changes. The mention buckets and resolved prose
// ledger are unchanged.
func reinstallMentionEdgeWriters(ctx context.Context, tx *sql.Tx) error {
	buckets := func(row, from string) string {
		return `INSERT INTO resource_access_mention_buckets SELECT ` + row + `rowid,j.value FROM ` + from + `json_each(anx_resource_mention_buckets(CAST(` + row + `target_ref AS BLOB))) j WHERE true ON CONFLICT DO NOTHING;`
	}
	resolve := `INSERT INTO resource_access_mentions SELECT NEW.rowid,i.identity_id,NEW.source_kind,NEW.source_id FROM resource_access_mention_buckets b JOIN resource_access_identities i ON i.bucket=b.bucket WHERE b.edge_id=NEW.rowid AND ` + resourceaccess.TextReferenceMatchSQL("NEW.target_ref", "(i.kind||':'||i.ref)") + ` ON CONFLICT DO NOTHING;`
	clear := `DELETE FROM resource_access_compact_edges WHERE edge_id=OLD.rowid; DELETE FROM resource_access_mentions WHERE edge_id=OLD.rowid; DELETE FROM resource_access_mention_buckets WHERE edge_id=OLD.rowid;`
	insert := `INSERT INTO resource_access_compact_edges VALUES(NEW.rowid,unhex(anx_resource_atom_key(CAST(NEW.target_ref AS BLOB)))) ON CONFLICT DO NOTHING;`
	// SQLite REPLACE may delete a colliding raw row without firing its DELETE
	// trigger. A BEFORE trigger records the exact rowid that the unique or
	// primary-key conflict will remove, so the AFTER trigger repairs only that
	// row. Searching the compact ledger by target atom instead would scan every
	// edge that shares a common atom (for example a series label key).
	conflict := `SELECT edge_id FROM resource_access_edge_conflict WHERE NOT EXISTS(SELECT 1 FROM resource_access_edges r WHERE r.rowid=resource_access_edge_conflict.edge_id)`
	repair := `DELETE FROM resource_access_mentions WHERE edge_id IN (` + conflict + `); DELETE FROM resource_access_mention_buckets WHERE edge_id IN (` + conflict + `); DELETE FROM resource_access_compact_edges WHERE edge_id IN (` + conflict + `); DELETE FROM resource_access_mentions WHERE edge_id=NEW.rowid; DELETE FROM resource_access_mention_buckets WHERE edge_id=NEW.rowid; DELETE FROM resource_access_compact_edges WHERE edge_id=NEW.rowid; `
	recordInsert := `DELETE FROM resource_access_edge_conflict; INSERT INTO resource_access_edge_conflict SELECT rowid FROM resource_access_edges WHERE (source_kind=NEW.source_kind AND source_id=NEW.source_id AND target_ref=NEW.target_ref) OR (NEW.rowid>0 AND rowid=NEW.rowid);`
	recordUpdate := `DELETE FROM resource_access_edge_conflict; INSERT INTO resource_access_edge_conflict SELECT rowid FROM resource_access_edges WHERE rowid<>OLD.rowid AND source_kind=NEW.source_kind AND source_id=NEW.source_id AND target_ref=NEW.target_ref;`
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS resource_access_edge_conflict(edge_id INTEGER NOT NULL)`,
		`DROP TRIGGER IF EXISTS access_mention_edge_conflict_insert`,
		`DROP TRIGGER IF EXISTS access_mention_edge_conflict_update`,
		`DROP TRIGGER IF EXISTS access_mention_edge_insert`,
		`DROP TRIGGER IF EXISTS access_mention_edge_update`,
		`DROP TRIGGER IF EXISTS access_mention_edge_delete`,
		`CREATE TRIGGER access_mention_edge_conflict_insert BEFORE INSERT ON resource_access_edges BEGIN ` + recordInsert + ` END`,
		`CREATE TRIGGER access_mention_edge_conflict_update BEFORE UPDATE ON resource_access_edges BEGIN ` + recordUpdate + ` END`,
		`CREATE TRIGGER access_mention_edge_insert AFTER INSERT ON resource_access_edges BEGIN ` + repair + buckets("NEW.", "") + insert + resolve + ` END`,
		`CREATE TRIGGER access_mention_edge_update AFTER UPDATE ON resource_access_edges BEGIN ` + clear + repair + buckets("NEW.", "") + insert + resolve + ` END`,
		`CREATE TRIGGER access_mention_edge_delete AFTER DELETE ON resource_access_edges BEGIN ` + clear + ` END`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("reinstall mention edge writers: %w", err)
		}
	}
	return nil
}

// reinstallAdmissionLedgerTriggers replaces the exact/compact origin triggers
// with one compact-ledger source. Both origins keep their independent ledger
// values so reachability and owner counts are unchanged.
func reinstallAdmissionLedgerTriggers(ctx context.Context, tx *sql.Tx) error {
	links := `INSERT OR IGNORE INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger) `
	publication := `('work_metadata','work_observation','work_evidence_record','work_evidence_alias')`
	refOrigin := func(p string) string {
		return `CASE WHEN ` + p + `id IS NULL THEN 'tuple:'||json_array(` + p + `source_type,` + p + `source_id,` + p + `target_type,` + p + `target_id,` + p + `edge_type) ELSE 'id:'||` + p + `id END`
	}
	keyLinks := links + `SELECT 'exact',e.rowid,NEW.key_id,NEW.kind,NEW.id,e.source_kind,e.source_id,1 FROM resource_access_compact_edges c JOIN resource_access_edges e ON e.rowid=c.edge_id WHERE c.target_key=NEW.atom AND (NEW.published=0 OR e.source_kind NOT IN ` + publication + `); ` +
		links + `SELECT 'compact',e.rowid,NEW.key_id,NEW.kind,NEW.id,e.source_kind,e.source_id,2 FROM resource_access_compact_edges c JOIN resource_access_edges e ON e.rowid=c.edge_id WHERE c.target_key=NEW.atom AND (NEW.published=0 OR e.source_kind NOT IN ` + publication + `); ` +
		links + `SELECT 'external',e.rowid,NEW.key_id,NEW.kind,NEW.id,e.source_kind,e.source_id,0 FROM resource_access_external_edges e WHERE NEW.published=1 AND e.target_key=lower(hex(NEW.atom)); ` +
		links + `SELECT 'ref',` + refOrigin("e.") + `,NEW.key_id,NEW.kind,NEW.id,e.source_type,e.source_id,0 FROM ref_edges e INDEXED BY idx_ref_edges_access_cover WHERE NEW.navigation=1 AND e.target_type=NEW.kind AND e.target_id=NEW.ref COLLATE NOCASE AND e.edge_type='ref'; ` +
		links + `SELECT 'project',m.card_id,NEW.key_id,NEW.kind,NEW.id,'card',m.card_id,0 FROM work_metadata m WHERE NEW.kind='topic' AND NEW.navigation=1 AND ` + resourceaccess.ReferenceSQL(`json_extract(m.metadata_json,'$.project_ref')`) + `=('topic:'||NEW.ref) COLLATE NOCASE;`
	queries := []string{
		`CREATE TRIGGER admission_key_insert AFTER INSERT ON resource_access_admission_keys BEGIN ` + keyLinks + ` END`,
		`CREATE TRIGGER admission_key_delete AFTER DELETE ON resource_access_admission_keys BEGIN DELETE FROM resource_access_admission_links WHERE origin IN ('exact','compact','external','ref','project') AND via_id=OLD.key_id; END`,
	}
	for _, spec := range []struct{ name, ledger string }{{"exact", "1"}, {"compact", "2"}} {
		insert := func(p string) string {
			return links + `SELECT '` + spec.name + `',` + p + `edge_id,k.key_id,k.kind,k.id,e.source_kind,e.source_id,` + spec.ledger + ` FROM resource_access_compact_edges c JOIN resource_access_edges e ON e.rowid=c.edge_id JOIN resource_access_admission_keys k ON k.atom=c.target_key WHERE c.edge_id=` + p + `edge_id AND (k.published=0 OR e.source_kind NOT IN ` + publication + `);`
		}
		clear := `DELETE FROM resource_access_admission_links WHERE origin='` + spec.name + `' AND origin_id=CAST(OLD.edge_id AS TEXT);`
		queries = append(queries,
			`CREATE TRIGGER admission_`+spec.name+`_insert AFTER INSERT ON resource_access_compact_edges BEGIN `+insert("NEW.")+` END`,
			`CREATE TRIGGER admission_`+spec.name+`_update AFTER UPDATE ON resource_access_compact_edges BEGIN `+clear+insert("NEW.")+` END`,
			`CREATE TRIGGER admission_`+spec.name+`_delete AFTER DELETE ON resource_access_compact_edges BEGIN `+clear+` END`,
		)
	}
	sourceInsert := func(p string) string {
		return links + `SELECT 'exact',` + p + `rowid,k.key_id,k.kind,k.id,` + p + `source_kind,` + p + `source_id,1 FROM resource_access_compact_edges c JOIN resource_access_admission_keys k ON k.atom=c.target_key WHERE c.edge_id=` + p + `rowid AND (k.published=0 OR ` + p + `source_kind NOT IN ` + publication + `); ` +
			links + `SELECT 'compact',` + p + `rowid,k.key_id,k.kind,k.id,` + p + `source_kind,` + p + `source_id,2 FROM resource_access_compact_edges c JOIN resource_access_admission_keys k ON k.atom=c.target_key WHERE c.edge_id=` + p + `rowid AND (k.published=0 OR ` + p + `source_kind NOT IN ` + publication + `);`
	}
	queries = append(queries,
		`CREATE TRIGGER admission_compact_source_insert AFTER INSERT ON resource_access_edges BEGIN DELETE FROM resource_access_admission_links WHERE origin IN ('exact','compact') AND origin_id=CAST(NEW.rowid AS TEXT); `+sourceInsert("NEW.")+` END`,
		`CREATE TRIGGER admission_compact_source_update AFTER UPDATE OF rowid,source_kind,source_id ON resource_access_edges BEGIN DELETE FROM resource_access_admission_links WHERE origin IN ('exact','compact') AND origin_id=CAST(OLD.rowid AS TEXT); `+sourceInsert("NEW.")+` END`,
		`CREATE TRIGGER admission_compact_source_delete AFTER DELETE ON resource_access_edges BEGIN DELETE FROM resource_access_admission_links WHERE origin IN ('exact','compact') AND origin_id=CAST(OLD.rowid AS TEXT); END`,
	)
	for _, q := range queries {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("reinstall admission ledger triggers: %w", err)
		}
	}
	return nil
}

// reinstallReceiptAccessTriggers rebuilds the receipt invalidation triggers so
// they probe the compact ledger instead of the retired exact ledger.
func reinstallReceiptAccessTriggers(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='trigger' AND name GLOB 'receipt_access_*'`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER "`+strings.ReplaceAll(name, `"`, `""`)+`"`); err != nil {
			return err
		}
	}
	return installReceiptStreamAccess(ctx, tx)
}

// installAdmissionLinkIndexes narrows the three admission link indexes to the
// only statements that read them. The child/parent indexes serve the four
// comment/document cleanup triggers; the via index serves key deletion.
func installAdmissionLinkIndexes(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`DROP INDEX IF EXISTS idx_access_admission_child`,
		`DROP INDEX IF EXISTS idx_access_admission_parent`,
		`DROP INDEX IF EXISTS idx_access_admission_via`,
		`CREATE INDEX idx_access_admission_child ON resource_access_admission_links(child_kind,child_id,ledger,parent_kind,parent_id) WHERE origin='comment'`,
		`CREATE INDEX idx_access_admission_parent ON resource_access_admission_links(parent_kind,parent_id,child_kind,child_id) WHERE origin='document_comment'`,
		`CREATE INDEX idx_access_admission_via ON resource_access_admission_links(origin,via_id) WHERE origin IN ('exact','compact','external','ref','project')`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("install admission link indexes: %w", err)
		}
	}
	return nil
}

// verifyRetiredAccessLedger is the recorded postcondition for migration 150.
func verifyRetiredAccessLedger(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"resource_access_exact_edges", "resource_access_admission_compact_atoms"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("retired access table still exists: %s", table)
		}
	}
	for _, index := range []string{"idx_access_admission_child", "idx_access_admission_parent", "idx_access_admission_via"} {
		var sql string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type='index' AND name=?`, index).Scan(&sql); err != nil {
			return err
		}
		if !strings.Contains(sql, " WHERE ") {
			return fmt.Errorf("admission index is not partial: %s", index)
		}
	}
	var reader, stable int
	if err := tx.QueryRowContext(ctx, `SELECT reader,stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&reader, &stable); err != nil {
		return err
	}
	if reader != 1 || stable != 1 {
		return fmt.Errorf("compact access is not the only reader: reader=%d stable=%d", reader, stable)
	}
	return nil
}
