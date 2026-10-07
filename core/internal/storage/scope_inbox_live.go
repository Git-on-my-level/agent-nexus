package storage

import (
	"context"
	"database/sql"
	"time"

	"agent-nexus-core/internal/inboxmodel"
	"agent-nexus-core/internal/sqliteutil"
)

func installScopeInboxLive(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "derived_inbox_items")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, inboxmodel.SchemaSQL)
	return err
}

// MaintainScopeInboxBatch shares the durable keyset maintenance pattern with
// lifecycle maintenance. Only 64 canonical rows are read/written per transaction.
// Concurrent imports/edits/deletes update the index atomically through triggers;
// therefore inserts behind the cursor cannot be missed. No historical payload
// is decoded or copied, and the final empty seek publishes readiness atomically.
func (w *Workspace) MaintainScopeInboxBatch(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(ctx, w.db)
	if err != nil {
		return false, err
	}
	defer cleanup()
	var cursor string
	var done bool
	if err = tx.QueryRowContext(ctx, `SELECT cursor,done FROM scope_inbox_live_job WHERE singleton=1`).Scan(&cursor, &done); err != nil {
		return false, err
	}
	if done {
		return true, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM derived_inbox_items WHERE id>? ORDER BY id LIMIT ?`, cursor, inboxmodel.BatchSize)
	if err != nil {
		return false, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO scope_inbox_live SELECT id,thread_id,`+inboxmodel.RankSQL+`,trigger_at FROM derived_inbox_items WHERE id=?`, id); err != nil {
			return false, err
		}
		cursor = id
	}
	done = len(ids) == 0
	if _, err = tx.ExecContext(ctx, `UPDATE scope_inbox_live_job SET cursor=?,done=? WHERE singleton=1`, cursor, done); err != nil {
		return false, err
	}
	return done, tx.Commit()
}

// RunScopeInboxMaintenance uses the server's maintenance lifecycle. Committed
// cursors survive restart; cancellation interrupts the current bounded batch.
func (w *Workspace) RunScopeInboxMaintenance(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastReport time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			done, err := w.MaintainScopeInboxBatch(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if report != nil && time.Since(lastReport) >= time.Minute {
					report(err)
					lastReport = time.Now()
				}
			} else if done {
				return
			}
		}
	}
}
