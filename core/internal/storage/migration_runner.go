package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Both loaders use this runner. A chunk never owns a ledger write: completing
// its steps, verifying the resulting schema, recording the version and updating
// a fenced digest are one transaction. Incomplete chunks commit only progress.
func applyMigrationToLedger(ctx context.Context, db *sql.DB, m migration, ledger string) error {
	if ledger != "schema_migrations" && ledger != "scope_schema_migrations" {
		return fmt.Errorf("invalid migration ledger %q", ledger)
	}
	if m.ApplyChunk != nil && (len(m.Statements) > 0 || m.AfterApply != nil) {
		return fmt.Errorf("migration %d mixes chunk and transactional steps", m.Version)
	}
	if m.ApplyChunk == nil && len(m.Statements) == 0 && m.AfterApply == nil {
		return fmt.Errorf("migration %d has no executable steps", m.Version)
	}
	finish := reportMigrationProgress(ctx, m.Version)
	defer finish()
	for {
		done, err := func() (bool, error) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return false, err
			}
			defer tx.Rollback()
			var recorded bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+ledger+` WHERE version=?)`, m.Version).Scan(&recorded); err != nil {
				return false, err
			}
			if recorded {
				if m.Verify != nil {
					if err := m.Verify(ctx, tx); err != nil {
						return false, err
					}
				}
				return true, nil
			}
			done := true
			if m.ApplyChunk != nil {
				done, err = m.ApplyChunk(ctx, tx)
				if err != nil {
					return false, err
				}
			} else {
				for _, statement := range m.Statements {
					if _, err := tx.ExecContext(ctx, statement); err != nil {
						return false, err
					}
				}
				if m.AfterApply != nil {
					if err := m.AfterApply(ctx, tx); err != nil {
						return false, err
					}
				}
			}
			if done {
				if m.Verify != nil {
					if err := m.Verify(ctx, tx); err != nil {
						return false, err
					}
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO `+ledger+`(version,applied_at) VALUES(?,CURRENT_TIMESTAMP)`, m.Version); err != nil {
					return false, err
				}
				if ledger == "scope_schema_migrations" {
					digest, err := scopeLedgerDigest(ctx, tx, ledger)
					if err != nil {
						return false, err
					}
					if _, err := tx.ExecContext(ctx, `UPDATE scope_format_state SET ledger_digest=? WHERE singleton=1`, digest); err != nil {
						return false, err
					}
				}
			}
			return done, tx.Commit()
		}()
		if err != nil {
			return fmt.Errorf("migration %d: %w", m.Version, err)
		}
		if done {
			return nil
		}
	}
}

func verifyRecordedMigration(ctx context.Context, db *sql.DB, m migration) error {
	if m.Verify == nil {
		return nil
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := m.Verify(ctx, tx); err != nil {
		return fmt.Errorf("recorded migration %d: %w", m.Version, err)
	}
	return nil
}

// A recorded 116 is usable only with all required admission tables, including
// page routing and the required compact mirror. Never hide an unexecuted step
// behind an advanced ledger; a previously malformed preview fails at startup.
func verifyAdmissionMigration(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{
		"resource_access_admission_compact_atoms", "resource_access_admission_keys", "resource_access_admission_links",
		"resource_access_admission_roots", "resource_access_admission_graph",
		"resource_access_admission_spellings", "resource_access_admission_prose_prefixes", "resource_access_admission_reach",
		"resource_access_admission_rebuild", "resource_access_admission_owners", "resource_access_admission_root_owners", "work_lifecycle_positions",
	} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("migration 116 admission table missing: %s", table)
		}
	}
	building, err := sqliteTableExists(ctx, tx, "resource_access_admission_build_state")
	if err != nil {
		return err
	}
	if building {
		return fmt.Errorf("migration 116 admission backfill incomplete")
	}
	return nil
}
