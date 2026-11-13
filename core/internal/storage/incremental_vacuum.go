package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"

	"agent-nexus-core/internal/sqliteutil"
)

const (
	defaultIncrementalVacuumMinFreelistBytes   int64 = 64 * 1024 * 1024
	defaultIncrementalVacuumMinFreelistPercent       = 10
	defaultIncrementalVacuumPages                    = 2048
	maxIncrementalVacuumPages                        = 65536
	defaultIncrementalVacuumBusyTimeout              = 250 * time.Millisecond
)

// incrementalVacuumConfig bounds online space reclamation. It only acts on
// databases built with auto_vacuum=INCREMENTAL; auto_vacuum=NONE is a no-op.
type incrementalVacuumConfig struct {
	enabled            bool
	minFreelistBytes   int64
	minFreelistPercent int
	pages              int
	busyTimeout        time.Duration
}

type incrementalVacuumStats struct {
	autoVacuum    int
	pageSize      int64
	pageCount     int64
	freelistCount int64
}

func (c incrementalVacuumConfig) shouldReclaim(stats incrementalVacuumStats) bool {
	if !c.enabled || stats.autoVacuum != 2 || stats.pageSize <= 0 || stats.freelistCount <= 0 {
		return false
	}
	if stats.freelistCount*stats.pageSize < c.minFreelistBytes {
		return false
	}
	return stats.freelistCount*100 >= stats.pageCount*int64(c.minFreelistPercent)
}

type incrementalVacuumResult struct {
	stats          incrementalVacuumStats
	freelistBefore int64
	reclaimed      int64
	skipped        bool
}

type incrementalVacuumExecutor interface {
	Reclaim(ctx context.Context, config incrementalVacuumConfig) (incrementalVacuumResult, error)
	Close(ctx context.Context) error
}

type sqliteIncrementalVacuumExecutor struct {
	conn *sql.Conn
}

func (e *sqliteIncrementalVacuumExecutor) Reclaim(ctx context.Context, config incrementalVacuumConfig) (incrementalVacuumResult, error) {
	var result incrementalVacuumResult
	restore, err := sqliteutil.SetConnectionBusyTimeout(ctx, e.conn, config.busyTimeout)
	if err != nil {
		return result, err
	}
	defer restore()
	before, err := readIncrementalVacuumStats(ctx, e.conn)
	if err != nil {
		return result, err
	}
	result.stats = before
	result.freelistBefore = before.freelistCount
	if !config.shouldReclaim(before) {
		result.skipped = true
		return result, nil
	}
	if _, err = e.conn.ExecContext(ctx, fmt.Sprintf("PRAGMA incremental_vacuum(%d)", config.pages)); err != nil {
		return result, err
	}
	after, err := readIncrementalVacuumStats(ctx, e.conn)
	if err != nil {
		return result, err
	}
	result.stats = after
	result.reclaimed = before.freelistCount - after.freelistCount
	if result.reclaimed < 0 {
		result.reclaimed = 0
	}
	return result, nil
}

func (e *sqliteIncrementalVacuumExecutor) Close(ctx context.Context) error {
	// A canceled tick must not return a connection whose context is already
	// done to the pool; mark it bad so database/sql discards it instead.
	if ctx.Err() != nil {
		_ = e.conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	return e.conn.Close()
}

func readIncrementalVacuumStats(ctx context.Context, conn *sql.Conn) (incrementalVacuumStats, error) {
	var stats incrementalVacuumStats
	if err := conn.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&stats.autoVacuum); err != nil {
		return stats, err
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&stats.pageSize); err != nil {
		return stats, err
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&stats.pageCount); err != nil {
		return stats, err
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&stats.freelistCount); err != nil {
		return stats, err
	}
	return stats, nil
}
