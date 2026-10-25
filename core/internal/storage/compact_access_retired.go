package storage

import (
	"agent-nexus-core/internal/loopstats"
	"context"
	"errors"
	"time"

	"agent-nexus-core/internal/sqliteutil"
)

// Retired raw edges are an immutable, non-serving authorization ledger after
// the verified rename. Drain them in short slices before dropping the empty
// table. Canonical ledgers, certification and operator rollback are untouched.
func (w *Workspace) maintainCompactAccessRetiredBatch(ctx context.Context) (bool, error) {
	ctx, finish := loopstats.Start(ctx, "compact_retired")
	defer finish()
	var exists bool
	if err := w.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='resource_access_unstable_edges')`).Scan(&exists); err != nil || !exists {
		return !exists, err
	}
	return w.compactAccessBatch.run(ctx, 200, func(ctx context.Context, limit int) (bool, error) {
		tx, cleanup, err := sqliteutil.BeginMaintenanceChunkWithBusyWait(ctx, w.db, 20*time.Millisecond)
		if err != nil {
			return false, err
		}
		defer cleanup()
		var exists, stable bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='resource_access_unstable_edges'),stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&exists, &stable); err != nil {
			return false, err
		}
		if !exists {
			return true, tx.Commit()
		}
		if !stable {
			return false, errors.New("retired access cleanup requires certified stable identities")
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM resource_access_unstable_edges WHERE rowid IN (SELECT rowid FROM resource_access_unstable_edges ORDER BY rowid LIMIT ?)`, limit)
		if err != nil {
			return false, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		loopstats.Rows(ctx, int(n))
		if n == 0 {
			if _, err = tx.ExecContext(ctx, `DROP TABLE resource_access_unstable_edges`); err != nil {
				return false, err
			}
		}
		return n == 0, tx.Commit()
	})
}

// RunCompactAccessRetiredMaintenance resumes cleanup across shutdown/restart.
// It owns no canonical data and never affects the selected reader.
func (w *Workspace) RunCompactAccessRetiredMaintenance(ctx context.Context) {
	for compactMigrationWait(ctx, 25*time.Millisecond) {
		var stable bool
		if err := w.db.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
			return
		}
		if !stable {
			if !compactMigrationWait(ctx, time.Second) {
				return
			}
			continue
		}
		done, err := w.maintainCompactAccessRetiredBatch(ctx)
		if done || (err != nil && !compactMigrationRetryable(err)) {
			return
		}
	}
}
