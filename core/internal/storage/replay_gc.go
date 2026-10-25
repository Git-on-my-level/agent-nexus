package storage

import (
	"agent-nexus-core/internal/loopstats"
	"context"
	"database/sql"
	"time"

	"agent-nexus-core/internal/replaypolicy"
	"agent-nexus-core/internal/sqliteutil"
)

// Receipt collection never runs in a request transaction. The indexed subquery
// selects at most one batch; each call releases the writer before yielding.
const replayGCQuery = `DELETE FROM idempotency_replays WHERE rowid IN (
 SELECT rowid FROM idempotency_replays WHERE created_at < ? ORDER BY created_at,rowid LIMIT ?
)`

func CollectReplayReceipts(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	ctx, finish := loopstats.Start(ctx, "replay_gc")
	defer finish()
	cutoff := now.UTC().Add(-replaypolicy.Retention()).Format("2006-01-02 15:04:05")
	var pending bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM idempotency_replays WHERE created_at < ? LIMIT 1)`, cutoff).Scan(&pending); err != nil || !pending {
		return 0, err
	}
	// The existence check holds no writer; re-evaluate the bounded selection in
	// the transaction. Contention yields immediately, rather than inheriting
	// the serving connection's 20-second busy handler. Never cancel its SQL.
	sqlCtx := context.WithoutCancel(ctx)
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(sqlCtx, db)
	if err != nil {
		return 0, err
	}
	defer cleanup()
	result, err := tx.ExecContext(sqlCtx, replayGCQuery, cutoff, replaypolicy.GCBatchSize())
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	loopstats.Rows(ctx, int(n))
	return n, nil
}

// RunReplayReceiptMaintenance stops on server shutdown. A backlog yields for a
// second between batches; an empty/failed pass waits a minute. Logs never expose
// private receipt contents or database errors to readers.
func RunReplayReceiptMaintenance(ctx context.Context, db *sql.DB, onError func(error)) {
	delay := time.Minute
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		batchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		n, err := CollectReplayReceipts(batchCtx, db, time.Now())
		cancel()
		delay = time.Minute
		if err != nil {
			if onError != nil && ctx.Err() == nil {
				onError(err)
			}
			continue
		}
		if n == int64(replaypolicy.GCBatchSize()) {
			delay = time.Second
		}
	}
}
