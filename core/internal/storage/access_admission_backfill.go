package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Linear schema/direct-index setup is separate from the potentially quadratic
// root x vertex backfill. Each later transaction admits exactly one root in all
// three ledgers and commits its cursor with those memberships. A chunk can visit
// that root's reachable component, just like an access write; it cannot multiply
// the work by every other root. Initialization never serves partial admission.
func applyAccessAdmissionMigration(ctx context.Context, db *sql.DB) error {
	for {
		done, err := accessAdmissionMigrationChunk(ctx, db)
		if err != nil {
			return fmt.Errorf("migration 116 admission backfill: %w", err)
		}
		if done {
			return nil
		}
	}
}

func accessAdmissionMigrationChunk(ctx context.Context, db *sql.DB) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var recorded bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=116)`).Scan(&recorded); err != nil {
		return false, err
	}
	if recorded {
		return true, nil
	}
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
		return false, tx.Commit()
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
	query += `GROUP BY kind,id ORDER BY kind,id LIMIT 1`
	var kind, id any
	err = tx.QueryRowContext(ctx, query, args...).Scan(&kind, &id)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `DROP TABLE resource_access_admission_build_state; INSERT INTO schema_migrations(version,applied_at) VALUES(116,CURRENT_TIMESTAMP)`); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	seed := `SELECT l.value,?,?,?,? FROM json_each('[-1,1,2]') l`
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_access_admission_reach `+admissionReachWalk(seed)+`SELECT * FROM reach WHERE true ON CONFLICT DO NOTHING`, kind, id, kind, id); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_access_admission_build_state SET last_kind=?,last_id=? WHERE singleton=1`, kind, id); err != nil {
		return false, err
	}
	return false, tx.Commit()
}
