package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func openIncrementalVacuumTestDB(t *testing.T, autoVacuum string) (*sql.DB, *sql.Conn) {
	t.Helper()
	path := t.TempDir() + "/state.sqlite"
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`PRAGMA page_size=4096`,
		`PRAGMA auto_vacuum=` + autoVacuum,
		`VACUUM`,
		`CREATE TABLE items(id INTEGER PRIMARY KEY, body BLOB)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5000; i++ {
		if _, err := tx.Exec(`INSERT INTO items(id, body) VALUES(?, ?)`, i, strings.Repeat("x", 1024)); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM items`); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return db, conn
}

func TestIncrementalVacuumReclaimsFreelistOnIncrementalDatabase(t *testing.T) {
	_, conn := openIncrementalVacuumTestDB(t, "INCREMENTAL")
	ctx := context.Background()
	before, err := readIncrementalVacuumStats(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if before.autoVacuum != 2 {
		t.Fatalf("auto_vacuum = %d, want 2", before.autoVacuum)
	}
	if before.freelistCount == 0 {
		t.Fatal("fixture has no freelist pages")
	}
	executor := &sqliteIncrementalVacuumExecutor{conn: conn}
	result, err := executor.Reclaim(ctx, incrementalVacuumConfig{
		enabled:            true,
		minFreelistBytes:   1,
		minFreelistPercent: 0,
		pages:              2048,
		busyTimeout:        250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.skipped {
		t.Fatal("incremental database was skipped")
	}
	if result.reclaimed <= 0 {
		t.Fatalf("reclaimed = %d, want > 0", result.reclaimed)
	}
	if result.stats.freelistCount >= before.freelistCount {
		t.Fatalf("freelist did not shrink: before=%d after=%d", before.freelistCount, result.stats.freelistCount)
	}
}

func TestIncrementalVacuumNoOpOnNoneDatabase(t *testing.T) {
	_, conn := openIncrementalVacuumTestDB(t, "NONE")
	ctx := context.Background()
	before, err := readIncrementalVacuumStats(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if before.autoVacuum != 0 {
		t.Fatalf("auto_vacuum = %d, want 0", before.autoVacuum)
	}
	executor := &sqliteIncrementalVacuumExecutor{conn: conn}
	result, err := executor.Reclaim(ctx, incrementalVacuumConfig{
		enabled:            true,
		minFreelistBytes:   1,
		minFreelistPercent: 0,
		pages:              2048,
		busyTimeout:        250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.skipped || result.reclaimed != 0 {
		t.Fatalf("NONE database was not a no-op: %+v", result)
	}
	after, err := readIncrementalVacuumStats(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if after.freelistCount != before.freelistCount {
		t.Fatalf("NONE freelist changed: before=%d after=%d", before.freelistCount, after.freelistCount)
	}
}

func TestIncrementalVacuumCloseWithCanceledContextDiscardsConnection(t *testing.T) {
	db, conn := openIncrementalVacuumTestDB(t, "INCREMENTAL")
	executor := &sqliteIncrementalVacuumExecutor{conn: conn}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Marking the connection bad can make the subsequent Close report
	// ErrConnDone; callers ignore Close errors, and the discard is what matters.
	if err := executor.Close(ctx); err != nil && !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("Close with canceled context: %v", err)
	}
	// A canceled-context close must return driver.ErrBadConn so database/sql
	// discards the connection instead of pooling it; an idle pooled connection
	// would leave the open connection count at one.
	if open := db.Stats().OpenConnections; open != 0 {
		t.Fatalf("open connections after canceled close = %d, want 0 (connection was pooled)", open)
	}
}

func TestIncrementalVacuumCloseWithLiveContextReturnsConnection(t *testing.T) {
	db, conn := openIncrementalVacuumTestDB(t, "INCREMENTAL")
	executor := &sqliteIncrementalVacuumExecutor{conn: conn}
	if err := executor.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if open := db.Stats().OpenConnections; open != 1 {
		t.Fatalf("open connections after close = %d, want 1 (idle pooled connection)", open)
	}
}

func TestIncrementalVacuumThresholds(t *testing.T) {
	config := incrementalVacuumConfig{
		enabled:            true,
		minFreelistBytes:   64 * 1024 * 1024,
		minFreelistPercent: 10,
		pages:              2048,
		busyTimeout:        250 * time.Millisecond,
	}
	for _, tc := range []struct {
		name  string
		stats incrementalVacuumStats
		want  bool
	}{
		{name: "none database", stats: incrementalVacuumStats{autoVacuum: 0, pageSize: 4096, pageCount: 100000, freelistCount: 100000}},
		{name: "below absolute", stats: incrementalVacuumStats{autoVacuum: 2, pageSize: 4096, pageCount: 100000, freelistCount: 1000}},
		{name: "below relative", stats: incrementalVacuumStats{autoVacuum: 2, pageSize: 4096, pageCount: 1000000, freelistCount: 20000}},
		{name: "both met", stats: incrementalVacuumStats{autoVacuum: 2, pageSize: 4096, pageCount: 100000, freelistCount: 20000}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := config.shouldReclaim(tc.stats); got != tc.want {
				t.Fatalf("shouldReclaim = %v, want %v", got, tc.want)
			}
		})
	}
	config.enabled = false
	if config.shouldReclaim(incrementalVacuumStats{autoVacuum: 2, pageSize: 4096, pageCount: 100000, freelistCount: 100000}) {
		t.Fatal("disabled config reclaimed")
	}
}
