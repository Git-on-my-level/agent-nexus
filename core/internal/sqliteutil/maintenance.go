package sqliteutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

// BeginMaintenanceChunk pins the worker's connection so SQLite's connection-local busy
// handler cannot outlive the chunk context. BEGIN IMMEDIATE fails immediately
// on contention; Run retries in a later slice. Serving connections keep their
// normal timeout. The caller must run cleanup on every exit, including Commit.
func BeginMaintenanceChunk(ctx context.Context, db *sql.DB) (*sql.Tx, func(), error) {
	return BeginMaintenanceChunkWithBusyWait(ctx, db, 0)
}

// SetConnectionBusyTimeout saves the connection-local busy timeout, sets wait,
// and returns a restore function. The restore function uses a short independent
// background timeout and invalidates the connection if restoration fails, so a
// worker-only timeout can never leak into serving. A write that fails after the
// original value was read is restored the same way before the error is returned:
// the driver can apply the pragma and still report failure.
func SetConnectionBusyTimeout(ctx context.Context, conn *sql.Conn, wait time.Duration) (func(), error) {
	var timeout int
	if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		return nil, err
	}
	restore := func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := conn.ExecContext(restoreCtx, fmt.Sprintf("PRAGMA busy_timeout=%d", timeout)); err != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", wait.Milliseconds())); err != nil {
		restore()
		return nil, err
	}
	return restore, nil
}

// BeginMaintenanceChunkWithBusyWait catches a short writer handoff without
// inheriting the serving busy handler. Admission wait is capped by the lease.
func BeginMaintenanceChunkWithBusyWait(ctx context.Context, db *sql.DB, wait time.Duration) (*sql.Tx, func(), error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		wait = min(wait, max(0, time.Until(deadline)))
	}
	wait = min(wait, 50*time.Millisecond)
	restore, err := SetConnectionBusyTimeout(ctx, conn, wait)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	var tx *sql.Tx
	released := false
	// Idempotent: a chunk may release its lock early and still defer cleanup.
	cleanup := func() {
		if released {
			return
		}
		released = true
		if tx != nil {
			tx.Rollback()
		}
		// The transaction's context may already be cancelled. Restore only on
		// this pinned connection, after rollback, before releasing it to DB.
		restore()
		conn.Close()
	}
	tx, err = conn.BeginTx(ctx, nil)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return tx, cleanup, nil
}
