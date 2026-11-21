package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/resourceaccess"
)

// Migration 150 completes the compact-access contraction (SCA-774). The legacy
// exact-edge ledger and the admission compact-atom mirror are derived copies of
// the compact ledger. Before either is dropped, the compact ledger is compared
// with the legacy ledger and the raw edge table in both EXCEPT directions. Any
// divergence aborts the upgrade; the migration never repairs or guesses.
//
// The migration is split into bounded, resumable phases. Each phase commits its
// own transaction and the runner checkpoints the WAL between phases, so a large
// workspace never accumulates the whole migration in one WAL. The phase cursor
// lives in resource_access_retirement_state until the final phase drops it.
const (
	retirePhaseInstall = iota
	retirePhaseExactDelete
	retirePhaseExactInsert
	retirePhaseCompactDelete
	retirePhaseCompactInsert
	retirePhaseCompactDone
	retirePhaseStableCreate
	retirePhaseStableCopy
	retirePhaseStableFinalize
	retirePhaseVerifyExact
	retirePhaseVerifyLedger1
	retirePhaseLedger1Delete
	retirePhaseLedger1Insert
	retirePhaseVerifyLedger2
	retirePhaseLedger2Delete
	retirePhaseLedger2Insert
	retirePhaseDropTables
	retirePhaseReinstallTriggers
	retirePhaseIndexChild
	retirePhaseIndexParent
	retirePhaseIndexVia
	retirePhaseFinalize
)

const (
	retireBatch      = 5000
	retireStateTable = `CREATE TABLE IF NOT EXISTS resource_access_retirement_state(singleton INTEGER PRIMARY KEY CHECK(singleton=1), phase INTEGER NOT NULL, cursor INTEGER)`
)

// retireLegacyAccessLedgerChunk executes one bounded phase. The runner commits
// it, checkpoints the WAL, and calls again until the final phase returns done.
func retireLegacyAccessLedgerChunk(ctx context.Context, tx *sql.Tx) (bool, error) {
	if _, err := tx.ExecContext(ctx, retireStateTable); err != nil {
		return false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_access_retirement_state(singleton,phase,cursor) VALUES(1,?,NULL) ON CONFLICT DO NOTHING`, retirePhaseInstall); err != nil {
		return false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	var phase int
	var cursor sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT phase,cursor FROM resource_access_retirement_state WHERE singleton=1`).Scan(&phase, &cursor); err != nil {
		return false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	next, nextCursor, done, err := runRetirementPhase(ctx, tx, phase, cursor)
	if err != nil {
		return false, err
	}
	if done {
		if _, err := tx.ExecContext(ctx, `DROP TABLE resource_access_retirement_state`); err != nil {
			return false, fmt.Errorf("retire legacy access ledger: %w", err)
		}
		return true, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_access_retirement_state SET phase=?,cursor=? WHERE singleton=1`, next, nextCursor); err != nil {
		return false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	return false, nil
}

func runRetirementPhase(ctx context.Context, tx *sql.Tx, phase int, cursor sql.NullInt64) (int, sql.NullInt64, bool, error) {
	switch phase {
	case retirePhaseInstall:
		return retireInstall(ctx, tx)
	case retirePhaseExactDelete:
		return retireDeleteAll(ctx, tx, "resource_access_exact_edges", phase, retirePhaseExactInsert)
	case retirePhaseExactInsert:
		return retireInsertExact(ctx, tx, cursor)
	case retirePhaseCompactDelete:
		return retireDeleteAll(ctx, tx, "resource_access_compact_edges", phase, retirePhaseCompactInsert)
	case retirePhaseCompactInsert:
		return retireInsertCompact(ctx, tx, cursor)
	case retirePhaseCompactDone:
		return retireCompactDone(ctx, tx)
	case retirePhaseStableCreate:
		return retireStableCreate(ctx, tx)
	case retirePhaseStableCopy:
		return retireStableCopy(ctx, tx, cursor)
	case retirePhaseStableFinalize:
		return retireStableFinalize(ctx, tx)
	case retirePhaseVerifyExact:
		if err := verifyLegacyExactLedgerEquivalent(ctx, tx); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		return retirePhaseVerifyLedger1, sql.NullInt64{}, false, nil
	case retirePhaseVerifyLedger1:
		ok, err := admissionLedgerMatches(ctx, tx, "exact", 1)
		if err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		if ok {
			return retirePhaseVerifyLedger2, sql.NullInt64{}, false, nil
		}
		return retirePhaseLedger1Delete, sql.NullInt64{}, false, nil
	case retirePhaseLedger1Delete:
		return retireDeleteAdmissionLedger(ctx, tx, "exact", phase, retirePhaseLedger1Insert)
	case retirePhaseLedger1Insert:
		return retireInsertAdmissionLedger(ctx, tx, "exact", 1, phase, retirePhaseVerifyLedger2, cursor)
	case retirePhaseVerifyLedger2:
		ok, err := admissionLedgerMatches(ctx, tx, "compact", 2)
		if err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		if ok {
			return retirePhaseDropTables, sql.NullInt64{}, false, nil
		}
		return retirePhaseLedger2Delete, sql.NullInt64{}, false, nil
	case retirePhaseLedger2Delete:
		return retireDeleteAdmissionLedger(ctx, tx, "compact", phase, retirePhaseLedger2Insert)
	case retirePhaseLedger2Insert:
		return retireInsertAdmissionLedger(ctx, tx, "compact", 2, phase, retirePhaseDropTables, cursor)
	case retirePhaseDropTables:
		for _, q := range []string{
			`DROP TABLE IF EXISTS resource_access_admission_compact_atoms`,
			`DROP TABLE IF EXISTS resource_access_exact_edges`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return 0, sql.NullInt64{}, false, fmt.Errorf("retire legacy access ledger: %w", err)
			}
		}
		return retirePhaseReinstallTriggers, sql.NullInt64{}, false, nil
	case retirePhaseReinstallTriggers:
		if err := reinstallRetiredAccessWriters(ctx, tx); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		return retirePhaseIndexChild, sql.NullInt64{}, false, nil
	case retirePhaseIndexChild:
		if err := createAdmissionLinkIndex(ctx, tx, "idx_access_admission_child"); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		return retirePhaseIndexParent, sql.NullInt64{}, false, nil
	case retirePhaseIndexParent:
		if err := createAdmissionLinkIndex(ctx, tx, "idx_access_admission_parent"); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		return retirePhaseIndexVia, sql.NullInt64{}, false, nil
	case retirePhaseIndexVia:
		if err := createAdmissionLinkIndex(ctx, tx, "idx_access_admission_via"); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		return retirePhaseFinalize, sql.NullInt64{}, false, nil
	case retirePhaseFinalize:
		if _, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET reader=1,stable=1,force_legacy=0 WHERE singleton=1`); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_job SET done=1 WHERE singleton=1`); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		if err := verifyRetiredAccessLedger(ctx, tx); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		return 0, sql.NullInt64{}, true, nil
	default:
		return 0, sql.NullInt64{}, false, fmt.Errorf("retire legacy access ledger: unknown phase %d", phase)
	}
}

// retireInstall installs the compact capability, stops the legacy writers, and
// chooses the first bounded phase. A certified compact ledger skips the rebuild.
func retireInstall(ctx context.Context, tx *sql.Tx) (int, sql.NullInt64, bool, error) {
	if err := ensureCompactAccessLedger(ctx, tx); err != nil {
		return 0, sql.NullInt64{}, false, err
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
			return 0, sql.NullInt64{}, false, fmt.Errorf("retire legacy access ledger: %w", err)
		}
	}
	exact, err := sqliteTableExists(ctx, tx, "resource_access_exact_edges")
	if err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	var done bool
	if err := tx.QueryRowContext(ctx, `SELECT done FROM resource_access_compact_job WHERE singleton=1`).Scan(&done); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if !exact && !done {
		// Migration 61 always creates the legacy ledger. Its absence with an
		// uncertified compact job means the workspace is corrupt; never mark an
		// incomplete compact ledger as certified.
		return 0, sql.NullInt64{}, false, errors.New("legacy exact ledger is missing before compact certification")
	}
	if exact && !done {
		return retirePhaseExactDelete, sql.NullInt64{}, false, nil
	}
	var stable bool
	if err := tx.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if !stable {
		return retirePhaseStableCreate, sql.NullInt64{}, false, nil
	}
	return retirePhaseVerifyExact, sql.NullInt64{}, false, nil
}

// retireDeleteAll removes one bounded batch from a derived ledger. Both tables
// have an INTEGER PRIMARY KEY, so rowid is the edge id.
func retireDeleteAll(ctx context.Context, tx *sql.Tx, table string, phase, next int) (int, sql.NullInt64, bool, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE rowid IN (SELECT rowid FROM `+table+` LIMIT ?)`, retireBatch); err != nil {
		return 0, sql.NullInt64{}, false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	var remaining bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+`)`).Scan(&remaining); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if remaining {
		return phase, sql.NullInt64{}, false, nil
	}
	return next, sql.NullInt64{}, false, nil
}

// retireBatchMax returns the last key of the next keyset batch. A NULL cursor
// starts before the minimum signed key, so imported negative edge ids are not
// skipped.
func retireBatchMax(ctx context.Context, tx *sql.Tx, table, column string, cursor sql.NullInt64) (sql.NullInt64, error) {
	q := `SELECT max(` + column + `) FROM (SELECT ` + column + ` FROM ` + table + ` ORDER BY ` + column + ` LIMIT ?)`
	args := []any{retireBatch}
	if cursor.Valid {
		q = `SELECT max(` + column + `) FROM (SELECT ` + column + ` FROM ` + table + ` WHERE ` + column + ` > ? ORDER BY ` + column + ` LIMIT ?)`
		args = []any{cursor.Int64, retireBatch}
	}
	var max sql.NullInt64
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&max); err != nil {
		return sql.NullInt64{}, err
	}
	return max, nil
}

func retireInsertExact(ctx context.Context, tx *sql.Tx, cursor sql.NullInt64) (int, sql.NullInt64, bool, error) {
	max, err := retireBatchMax(ctx, tx, "resource_access_edges", "rowid", cursor)
	if err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if !max.Valid {
		return retirePhaseCompactDelete, sql.NullInt64{}, false, nil
	}
	q := `INSERT INTO resource_access_exact_edges SELECT rowid,source_kind,source_id,anx_resource_atom_key(CAST(target_ref AS BLOB)) FROM resource_access_edges WHERE rowid <= ? ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_key=excluded.target_key`
	args := []any{max.Int64}
	if cursor.Valid {
		q = `INSERT INTO resource_access_exact_edges SELECT rowid,source_kind,source_id,anx_resource_atom_key(CAST(target_ref AS BLOB)) FROM resource_access_edges WHERE rowid > ? AND rowid <= ? ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_key=excluded.target_key`
		args = []any{cursor.Int64, max.Int64}
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return 0, sql.NullInt64{}, false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	return retirePhaseExactInsert, max, false, nil
}

func retireInsertCompact(ctx context.Context, tx *sql.Tx, cursor sql.NullInt64) (int, sql.NullInt64, bool, error) {
	max, err := retireBatchMax(ctx, tx, "resource_access_exact_edges", "edge_id", cursor)
	if err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if !max.Valid {
		return retirePhaseCompactDone, sql.NullInt64{}, false, nil
	}
	q := `INSERT INTO resource_access_compact_edges SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE edge_id <= ? ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key`
	args := []any{max.Int64}
	if cursor.Valid {
		q = `INSERT INTO resource_access_compact_edges SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE edge_id > ? AND edge_id <= ? ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key`
		args = []any{cursor.Int64, max.Int64}
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return 0, sql.NullInt64{}, false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	return retirePhaseCompactInsert, max, false, nil
}

func retireCompactDone(ctx context.Context, tx *sql.Tx) (int, sql.NullInt64, bool, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_job SET done=1 WHERE singleton=1`); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	var stable bool
	if err := tx.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if !stable {
		return retirePhaseStableCreate, sql.NullInt64{}, false, nil
	}
	return retirePhaseVerifyExact, sql.NullInt64{}, false, nil
}

func retireStableCreate(ctx context.Context, tx *sql.Tx) (int, sql.NullInt64, bool, error) {
	var stable bool
	if err := tx.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if stable {
		return retirePhaseVerifyExact, sql.NullInt64{}, false, nil
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS resource_access_stable_edges(edge_id INTEGER PRIMARY KEY,source_kind TEXT NOT NULL,source_id TEXT NOT NULL,target_ref TEXT NOT NULL COLLATE NOCASE,UNIQUE(source_kind,source_id,target_ref))`); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	return retirePhaseStableCopy, sql.NullInt64{}, false, nil
}

func retireStableCopy(ctx context.Context, tx *sql.Tx, cursor sql.NullInt64) (int, sql.NullInt64, bool, error) {
	var rawDone bool
	if err := tx.QueryRowContext(ctx, `SELECT raw_done FROM resource_access_compact_control WHERE singleton=1`).Scan(&rawDone); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if rawDone {
		return retirePhaseStableFinalize, sql.NullInt64{}, false, nil
	}
	max, err := retireBatchMax(ctx, tx, "resource_access_edges", "rowid", cursor)
	if err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if !max.Valid {
		if _, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET raw_done=1 WHERE singleton=1`); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		return retirePhaseStableFinalize, sql.NullInt64{}, false, nil
	}
	q := `INSERT INTO resource_access_stable_edges SELECT rowid,source_kind,source_id,target_ref FROM resource_access_edges WHERE rowid <= ? ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_ref=excluded.target_ref`
	args := []any{max.Int64}
	if cursor.Valid {
		q = `INSERT INTO resource_access_stable_edges SELECT rowid,source_kind,source_id,target_ref FROM resource_access_edges WHERE rowid > ? AND rowid <= ? ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_ref=excluded.target_ref`
		args = []any{cursor.Int64, max.Int64}
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET raw_cursor=? WHERE singleton=1`, max.Int64); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	return retirePhaseStableCopy, max, false, nil
}

func retireStableFinalize(ctx context.Context, tx *sql.Tx) (int, sql.NullInt64, bool, error) {
	old := `SELECT rowid,source_kind,source_id,CAST(target_ref AS BLOB) FROM resource_access_edges`
	staged := `SELECT edge_id,source_kind,source_id,CAST(target_ref AS BLOB) FROM resource_access_stable_edges`
	for _, q := range []string{`SELECT EXISTS(` + old + ` EXCEPT ` + staged + `)`, `SELECT EXISTS(` + staged + ` EXCEPT ` + old + `)`} {
		var mismatch bool
		if err := tx.QueryRowContext(ctx, q).Scan(&mismatch); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
		if mismatch {
			return 0, sql.NullInt64{}, false, errors.New("staged raw edge identities differ from the live table")
		}
	}
	if err := stabilizeAccessEdgesTx(ctx, tx); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	// A fresh workspace's retired table is empty; drop it now so idle
	// maintenance never needs a writer just to discover there is nothing to do.
	var empty bool
	if err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM resource_access_unstable_edges)`).Scan(&empty); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if empty {
		if _, err := tx.ExecContext(ctx, `DROP TABLE resource_access_unstable_edges`); err != nil {
			return 0, sql.NullInt64{}, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET stable=1 WHERE singleton=1`); err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	return retirePhaseVerifyExact, sql.NullInt64{}, false, nil
}

// admissionLedgerMatches compares one admission ledger with the rows derived
// from the verified compact ledger. It is read-only and builds only temp
// b-trees, so it never forces a graph/reach recomputation.
func admissionLedgerMatches(ctx context.Context, tx *sql.Tx, origin string, ledger int) (bool, error) {
	publication := `('work_metadata','work_observation','work_evidence_record','work_evidence_alias')`
	expected := `SELECT '` + origin + `',CAST(e.rowid AS TEXT),k.key_id,k.kind,k.id,e.source_kind,e.source_id,` + strconv.Itoa(ledger) + `
FROM resource_access_compact_edges c
JOIN resource_access_edges e ON e.rowid=c.edge_id
JOIN resource_access_admission_keys k ON k.atom=c.target_key
WHERE k.published=0 OR e.source_kind NOT IN ` + publication
	actual := `SELECT origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger FROM resource_access_admission_links WHERE origin='` + origin + `'`
	for _, q := range []string{
		`SELECT EXISTS(` + actual + ` EXCEPT ` + expected + `)`,
		`SELECT EXISTS(` + expected + ` EXCEPT ` + actual + `)`,
	} {
		var mismatch bool
		if err := tx.QueryRowContext(ctx, q).Scan(&mismatch); err != nil {
			return false, err
		}
		if mismatch {
			return false, nil
		}
	}
	return true, nil
}

func retireDeleteAdmissionLedger(ctx context.Context, tx *sql.Tx, origin string, phase, next int) (int, sql.NullInt64, bool, error) {
	result, err := tx.ExecContext(ctx, `DELETE FROM resource_access_admission_links
WHERE origin=? AND (origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger) IN (
  SELECT origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger
  FROM resource_access_admission_links WHERE origin=? LIMIT ?
)`, origin, origin, retireBatch)
	if err != nil {
		return 0, sql.NullInt64{}, false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if n > 0 {
		return phase, sql.NullInt64{}, false, nil
	}
	return next, sql.NullInt64{}, false, nil
}

func retireInsertAdmissionLedger(ctx context.Context, tx *sql.Tx, origin string, ledger, phase, next int, cursor sql.NullInt64) (int, sql.NullInt64, bool, error) {
	max, err := retireBatchMax(ctx, tx, "resource_access_compact_edges", "edge_id", cursor)
	if err != nil {
		return 0, sql.NullInt64{}, false, err
	}
	if !max.Valid {
		return next, sql.NullInt64{}, false, nil
	}
	publication := `('work_metadata','work_observation','work_evidence_record','work_evidence_alias')`
	q := `INSERT OR IGNORE INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger)
SELECT ?,CAST(e.rowid AS TEXT),k.key_id,k.kind,k.id,e.source_kind,e.source_id,?
FROM resource_access_compact_edges c
JOIN resource_access_edges e ON e.rowid=c.edge_id
JOIN resource_access_admission_keys k ON k.atom=c.target_key
WHERE c.edge_id <= ? AND (k.published=0 OR e.source_kind NOT IN ` + publication + `)`
	args := []any{origin, ledger, max.Int64}
	if cursor.Valid {
		q = `INSERT OR IGNORE INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger)
SELECT ?,CAST(e.rowid AS TEXT),k.key_id,k.kind,k.id,e.source_kind,e.source_id,?
FROM resource_access_compact_edges c
JOIN resource_access_edges e ON e.rowid=c.edge_id
JOIN resource_access_admission_keys k ON k.atom=c.target_key
WHERE c.edge_id > ? AND c.edge_id <= ? AND (k.published=0 OR e.source_kind NOT IN ` + publication + `)`
		args = []any{origin, ledger, cursor.Int64, max.Int64}
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return 0, sql.NullInt64{}, false, fmt.Errorf("retire legacy access ledger: %w", err)
	}
	return phase, max, false, nil
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

// reinstallRetiredAccessWriters replaces the retired exact/compact writers with
// the compact-ledger source and narrows the admission link indexes.
func reinstallRetiredAccessWriters(ctx context.Context, tx *sql.Tx) error {
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
	for _, q := range []string{
		`DROP INDEX IF EXISTS idx_access_admission_child`,
		`DROP INDEX IF EXISTS idx_access_admission_parent`,
		`DROP INDEX IF EXISTS idx_access_admission_via`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("install admission link indexes: %w", err)
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
			// Reference NEW.target_key directly so the update-program narrowing
			// includes target_key in its UPDATE OF/WHEN inputs. Joining through
			// the table alias would hide the changed column from that analysis.
			return links + `SELECT '` + spec.name + `',` + p + `edge_id,k.key_id,k.kind,k.id,e.source_kind,e.source_id,` + spec.ledger + ` FROM resource_access_edges e JOIN resource_access_admission_keys k ON k.atom=` + p + `target_key WHERE e.rowid=` + p + `edge_id AND (k.published=0 OR e.source_kind NOT IN ` + publication + `);`
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

// createAdmissionLinkIndex creates one narrowed index in its own phase so the
// WAL for a large index build is checkpointed before the next one starts.
func createAdmissionLinkIndex(ctx context.Context, tx *sql.Tx, name string) error {
	var q string
	switch name {
	case "idx_access_admission_child":
		q = `CREATE INDEX IF NOT EXISTS idx_access_admission_child ON resource_access_admission_links(child_kind,child_id,ledger,parent_kind,parent_id) WHERE origin='comment'`
	case "idx_access_admission_parent":
		q = `CREATE INDEX IF NOT EXISTS idx_access_admission_parent ON resource_access_admission_links(parent_kind,parent_id,child_kind,child_id) WHERE origin='document_comment'`
	case "idx_access_admission_via":
		q = `CREATE INDEX IF NOT EXISTS idx_access_admission_via ON resource_access_admission_links(origin,via_id) WHERE origin IN ('exact','compact','external','ref','project')`
	default:
		return fmt.Errorf("unknown admission link index %s", name)
	}
	if _, err := tx.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("install admission link index %s: %w", name, err)
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
	for _, trigger := range []string{
		"access_mention_edge_conflict_insert",
		"access_mention_edge_conflict_update",
		"access_mention_edge_insert",
		"access_mention_edge_update",
		"access_mention_edge_delete",
		"admission_key_insert",
		"admission_key_delete",
		"admission_exact_insert",
		"admission_exact_update",
		"admission_exact_delete",
		"admission_compact_insert",
		"admission_compact_update",
		"admission_compact_delete",
		"admission_compact_source_insert",
		"admission_compact_source_update",
		"admission_compact_source_delete",
	} {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='trigger' AND name=?)`, trigger).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("retired access trigger missing: %s", trigger)
		}
	}
	var reader, stable, forceLegacy int
	if err := tx.QueryRowContext(ctx, `SELECT reader,stable,force_legacy FROM resource_access_compact_control WHERE singleton=1`).Scan(&reader, &stable, &forceLegacy); err != nil {
		return err
	}
	if reader != 1 || stable != 1 || forceLegacy != 0 {
		return fmt.Errorf("compact access is not the only reader: reader=%d stable=%d force_legacy=%d", reader, stable, forceLegacy)
	}
	return nil
}

// checkpointMigrationWAL bounds migration WAL growth. Migration 150 commits
// bounded phases; this truncates the WAL between them so a large workspace
// never accumulates the whole migration in one WAL.
func checkpointMigrationWAL(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	// Discard this connection on exit: the zero busy timeout is for the
	// checkpoint only and must not leak into serving requests.
	defer conn.Raw(func(any) error { return driver.ErrBadConn })
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout=0`); err != nil {
		return err
	}
	for attempt := 0; attempt < 5; attempt++ {
		var busy, logPages, checkpointed int
		if err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logPages, &checkpointed); err != nil {
			return err
		}
		if busy == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return errors.New("WAL checkpoint remained busy during migration")
}
