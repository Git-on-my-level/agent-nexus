package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

// retirementFenceDigest marks a workspace whose migration 150 has committed at
// least one destructive phase but has not recorded version 150. A pre-150
// binary's applyWorkspaceMigrations refuses any non-empty scope_format_state
// without a fenced ledger, so a crash cannot leave a partial state that an
// older binary silently misreads.
const retirementFenceDigest = "anx-migration-150-in-progress"

// installRetirementFence runs in the first committed phase, before any legacy
// table is dropped. It is idempotent so a resumed migration keeps the fence.
func installRetirementFence(ctx context.Context, tx *sql.Tx) error {
	var fenced int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name='scope_schema_migrations' AND type='table'`).Scan(&fenced); err != nil {
		return err
	}
	if fenced > 0 {
		// The scope fence owns scope_format_state. Replacing its digest makes a
		// pre-150 binary fail the ledger integrity check.
		result, err := tx.ExecContext(ctx, `UPDATE scope_format_state SET ledger_digest=? WHERE singleton=1`, retirementFenceDigest)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return errors.New("scope format state missing")
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS scope_format_state(singleton INTEGER PRIMARY KEY CHECK(singleton=1),format INTEGER NOT NULL CHECK(format=1),ledger_digest TEXT NOT NULL,reader TEXT NOT NULL CHECK(reader='legacy'))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO scope_format_state VALUES(1,1,?,'legacy') ON CONFLICT(singleton) DO UPDATE SET ledger_digest=excluded.ledger_digest`, retirementFenceDigest); err != nil {
		return err
	}
	return nil
}

// clearRetirementFence runs in the final phase, in the same transaction that
// records version 150. A crash before that commit rolls the clear back, so the
// fence remains until the migration is complete.
func clearRetirementFence(ctx context.Context, tx *sql.Tx) error {
	var fenced int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name='scope_schema_migrations' AND type='table'`).Scan(&fenced); err != nil {
		return err
	}
	if fenced > 0 {
		// The runner replaces the sentinel with the real ledger digest when it
		// records 150 in this same transaction.
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS scope_format_state`); err != nil {
		return err
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
