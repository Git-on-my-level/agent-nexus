package storage

import (
	"context"
	"database/sql"
)

// Linear schema/direct-index setup is separate from the potentially quadratic
// root x vertex backfill. Each later transaction admits one bounded batch of
// roots in all three ledgers and commits its cursor with those memberships.
// A chunk can visit each batched root's reachable component, just like an
// access write; it cannot multiply the work by every other root. Initialization
// never serves partial admission.
//
// Batching amortizes fixed per-transaction cost: preparing the reach insert
// compiles its row triggers again, and every commit syncs the WAL. A migration
// that visits thousands of roots would otherwise pay both once per root.
var accessAdmissionRootBatch = 64

func accessAdmissionMigrationChunk(ctx context.Context, tx *sql.Tx) (bool, error) {
	exists, err := sqliteTableExists(ctx, tx, "resource_access_admission_build_state")
	if err != nil {
		return false, err
	}
	if !exists {
		if err := installAccessAdmission(ctx, tx); err != nil {
			return false, err
		}
		// Keep the original SQLite cursor value types. A TEXT affinity could
		// change legacy binary IDs and therefore the primary-key seek order.
		if _, err := tx.ExecContext(ctx, `CREATE TABLE resource_access_admission_build_state(singleton INTEGER PRIMARY KEY CHECK(singleton=1),last_kind,last_id,CHECK((last_kind IS NULL)=(last_id IS NULL))); INSERT INTO resource_access_admission_build_state VALUES(1,NULL,NULL)`); err != nil {
			return false, err
		}
		return false, nil
	}
	var lastKind, lastID any
	if err := tx.QueryRowContext(ctx, `SELECT last_kind,last_id FROM resource_access_admission_build_state WHERE singleton=1`).Scan(&lastKind, &lastID); err != nil {
		return false, err
	}
	query := `SELECT kind,id FROM resource_access_admission_roots `
	var args []any
	if lastKind != nil {
		query += `WHERE (kind,id)>(?,?) `
		args = []any{lastKind, lastID}
	}
	query += `GROUP BY kind,id ORDER BY kind,id LIMIT ?`
	args = append(args, max(accessAdmissionRootBatch, 1))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	var kinds, ids []any
	for rows.Next() {
		var kind, id any
		if err := rows.Scan(&kind, &id); err != nil {
			rows.Close()
			return false, err
		}
		kinds = append(kinds, kind)
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(kinds) == 0 {
		if _, err := tx.ExecContext(ctx, `DROP TABLE resource_access_admission_build_state`); err != nil {
			return false, err
		}
		return true, nil
	}
	// The batch is exactly the distinct roots after the cursor through its last
	// root. Its seed rows are the same (ledger, root) memberships as before.
	upperKind, upperID := kinds[len(kinds)-1], ids[len(ids)-1]
	bounds := `(kind,id)<=(?,?)`
	seedArgs := []any{upperKind, upperID}
	if lastKind != nil {
		bounds = `(kind,id)>(?,?) AND (kind,id)<=(?,?)`
		seedArgs = []any{lastKind, lastID, upperKind, upperID}
	}
	seed := `SELECT l.value,r.kind,r.id,r.kind,r.id FROM (SELECT kind,id FROM resource_access_admission_roots WHERE ` + bounds + ` GROUP BY kind,id) r CROSS JOIN json_each('[-1,1,2]') l`
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_access_admission_reach `+admissionReachWalk(seed)+`SELECT * FROM reach WHERE true ON CONFLICT DO NOTHING`, seedArgs...); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_access_admission_build_state SET last_kind=?,last_id=? WHERE singleton=1`, upperKind, upperID); err != nil {
		return false, err
	}
	return false, nil
}
