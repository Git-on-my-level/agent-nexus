package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

const scopeFenceViewSQL = `CREATE VIEW schema_migrations AS SELECT version,applied_at FROM scope_schema_migrations WHERE anx_requires_scope_format_v1()`

// InstallScopeFormatFence is deliberately NOT called by startup. The release
// coordinator must stop all pre-bridge processes/connections before invoking it.
// A Workspace's process lock excludes compatible servers; it cannot exclude a
// historical binary which never participates in that protocol. This first
// fence retains legacy authority; selecting scope authority belongs to A and
// requires extending the format/reader validation alongside all cutover gates.
func (w *Workspace) InstallScopeFormatFence(ctx context.Context) error {
	if w == nil || w.db == nil || w.processLock == nil {
		return errors.New("workspace serving lease required")
	}
	if err := w.EnableScopeServingLease(ctx); err != nil {
		return err
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM scope_format_state`).Scan(&existing); err != nil {
		return err
	}
	if existing != 0 {
		return errors.New("scope format fence already installed")
	}
	digest, err := scopeLedgerDigest(ctx, tx, "schema_migrations")
	if err != nil {
		return err
	}
	for _, statement := range []string{`ALTER TABLE schema_migrations RENAME TO scope_schema_migrations`, scopeFenceViewSQL} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO scope_format_state VALUES(1,1,?,'legacy')`, digest); err != nil {
		return err
	}
	return tx.Commit()
}

// applyWorkspaceMigrations reads the renamed ledger explicitly. It never
// registers the missing function or restores the old ledger: old binaries must
// continue to refuse this database on every subsequent open.
func applyWorkspaceMigrations(ctx context.Context, db *sql.DB) error {
	var ledgerType, oldType, viewSQL string
	err := db.QueryRowContext(ctx, `SELECT type FROM sqlite_schema WHERE name='scope_schema_migrations'`).Scan(&ledgerType)
	if errors.Is(err, sql.ErrNoRows) {
		// Expand-only installations have an empty state table. A committed
		// format marker without its ledger is corruption, never a legacy DB.
		var stateTable int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name='scope_format_state'`).Scan(&stateTable); err != nil {
			return err
		}
		if stateTable != 0 {
			var markers int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM scope_format_state`).Scan(&markers); err != nil {
				return err
			}
			if markers != 0 {
				return errors.New("fenced scope migration ledger missing")
			}
		}
		// Unknown views fail through the historical loader as well.
		return applyMigrations(ctx, db)
	}
	if err != nil {
		return err
	}
	if ledgerType != "table" {
		return errors.New("invalid scope migration ledger")
	}
	if err := db.QueryRowContext(ctx, `SELECT type,sql FROM sqlite_schema WHERE name='schema_migrations'`).Scan(&oldType, &viewSQL); err != nil {
		return err
	}
	if oldType != "view" || viewSQL != scopeFenceViewSQL {
		return errors.New("invalid scope downgrade fence")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var format int
	var storedDigest, reader string
	if err := tx.QueryRowContext(ctx, `SELECT format,ledger_digest,reader FROM scope_format_state WHERE singleton=1`).Scan(&format, &storedDigest, &reader); err != nil {
		return err
	}
	if format != 1 || reader != "legacy" {
		return errors.New("unsupported scope reader format")
	}
	digest, err := scopeLedgerDigest(ctx, tx, "scope_schema_migrations")
	if err != nil {
		return err
	}
	if digest != storedDigest {
		return errors.New("scope migration ledger integrity mismatch")
	}
	rows, err := tx.QueryContext(ctx, `SELECT version FROM scope_schema_migrations`)
	if err != nil {
		return err
	}
	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err = rows.Scan(&version); err != nil {
			break
		}
		applied[version] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	known := map[int]bool{}
	for _, m := range migrations {
		known[m.Version] = true
	}
	for version := range applied {
		if !known[version] {
			return fmt.Errorf("unsupported scope migration version %d", version)
		}
	}
	// New compatible migrations and their ledger digest commit atomically. This
	// is bounded by the migration registry, never historical workspace content.
	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}
		for _, statement := range m.Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("scope migration %d: %w", m.Version, err)
			}
		}
		if m.AfterApply != nil {
			if err := m.AfterApply(ctx, tx); err != nil {
				return fmt.Errorf("scope migration %d hook: %w", m.Version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO scope_schema_migrations VALUES(?,CURRENT_TIMESTAMP)`, m.Version); err != nil {
			return err
		}
	}
	digest, err = scopeLedgerDigest(ctx, tx, "scope_schema_migrations")
	if err != nil {
		return err
	}
	// An unchanged fenced reopen must not fire the authority-capture UPDATE
	// trigger and invalidate an otherwise resumable staged generation.
	if digest != storedDigest {
		if _, err := tx.ExecContext(ctx, `UPDATE scope_format_state SET ledger_digest=? WHERE singleton=1`, digest); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// This digest protects recorded ledger evidence, NOT historical migration
// source hashes (the released ledger has none). A's registry owns code/schema
// compatibility; unknown versions and unknown selected readers fail closed.
func scopeLedgerDigest(ctx context.Context, tx *sql.Tx, table string) (string, error) {
	if table != "schema_migrations" && table != "scope_schema_migrations" {
		return "", errors.New("invalid ledger")
	}
	rows, err := tx.QueryContext(ctx, `SELECT version,applied_at FROM `+table+` ORDER BY version`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	h := sha256.New()
	n := 0
	for rows.Next() {
		var version int
		var at string
		if err := rows.Scan(&version, &at); err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%d:%d:%s\n", version, len(at), at)
		n++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		return "", errors.New("empty migration ledger")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
