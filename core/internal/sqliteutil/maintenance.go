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
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	var timeout int
	if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		conn.Close()
		return nil, nil, err
	}
	var tx *sql.Tx
	cleanup := func() {
		if tx != nil {
			tx.Rollback()
		}
		// The transaction's context may already be cancelled. Restore only on
		// this pinned connection, after rollback, before releasing it to DB.
		restoreCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := conn.ExecContext(restoreCtx, fmt.Sprintf("PRAGMA busy_timeout=%d", timeout)); err != nil {
			// A failed restoration must never leak a worker timeout into serving.
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		conn.Close()
	}
	if _, err = conn.ExecContext(ctx, `PRAGMA busy_timeout=0`); err != nil {
		cleanup()
		return nil, nil, err
	}
	tx, err = conn.BeginTx(ctx, nil)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return tx, cleanup, nil
}
